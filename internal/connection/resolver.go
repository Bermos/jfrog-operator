// Package connection turns Connection and ClusterConnection objects into
// ready-to-use JFrog clients.
package connection

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/Bermos/jfrog-operator/api/common/v1alpha1"
	platformv1alpha1 "github.com/Bermos/jfrog-operator/api/platform/v1alpha1"
	"github.com/Bermos/jfrog-operator/internal/jfrog"
)

// Resolver resolves connection references to JFrog clients. Clients are
// cached and rebuilt only when the connection or one of its Secrets changes,
// so that HTTP connections are reused across reconciles.
type Resolver struct {
	Client client.Client
	// HTTPTimeout is the timeout of each request. Defaults to 30s.
	HTTPTimeout time.Duration

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	fingerprint string
	client      *jfrog.Client
}

// Resolve returns a client for the connection that ref points to. namespace is
// the namespace of the referencing object and is used for namespaced Connections.
func (r *Resolver) Resolve(ctx context.Context, namespace string, ref commonv1alpha1.ConnectionReference) (*jfrog.Client, error) {
	switch ref.Kind {
	case commonv1alpha1.ConnectionKind:
		var conn platformv1alpha1.Connection
		if err := r.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.Name}, &conn); err != nil {
			return nil, fmt.Errorf("getting Connection %s/%s: %w", namespace, ref.Name, err)
		}
		return r.ForConnection(ctx, &conn)
	case commonv1alpha1.ClusterConnectionKind, "":
		var conn platformv1alpha1.ClusterConnection
		if err := r.Client.Get(ctx, types.NamespacedName{Name: ref.Name}, &conn); err != nil {
			return nil, fmt.Errorf("getting ClusterConnection %s: %w", ref.Name, err)
		}
		return r.ForClusterConnection(ctx, &conn)
	default:
		return nil, fmt.Errorf("unsupported connection kind %q", ref.Kind)
	}
}

// ForConnection returns a client for a namespaced Connection.
func (r *Resolver) ForConnection(ctx context.Context, conn *platformv1alpha1.Connection) (*jfrog.Client, error) {
	return r.build(ctx, "Connection/"+conn.Namespace+"/"+conn.Name, conn.ResourceVersion, conn.Namespace, false, conn.Spec)
}

// ForClusterConnection returns a client for a ClusterConnection.
func (r *Resolver) ForClusterConnection(ctx context.Context, conn *platformv1alpha1.ClusterConnection) (*jfrog.Client, error) {
	ns := conn.Spec.Credentials.SecretRef.Namespace
	if ns == "" {
		return nil, fmt.Errorf("ClusterConnection %s: spec.credentials.secretRef.namespace is required", conn.Name)
	}
	return r.build(ctx, "ClusterConnection/"+conn.Name, conn.ResourceVersion, ns, true, conn.Spec)
}

// build creates or reuses a client. Secrets are read from secretNS; when
// crossNamespace is set, the CA Secret may name its own namespace.
func (r *Resolver) build(ctx context.Context, cacheID, version, secretNS string, crossNamespace bool, spec platformv1alpha1.ConnectionSpec) (*jfrog.Client, error) {
	creds, err := r.getSecret(ctx, secretNS, spec.Credentials.SecretRef.Name)
	if err != nil {
		return nil, err
	}
	fp := sha256.New()
	fmt.Fprintf(fp, "%s|%s|%s|", version, creds.ResourceVersion, creds.UID)

	cfg := jfrog.Config{
		URL:            spec.URL,
		ArtifactoryURL: spec.ArtifactoryURL,
		AccessURL:      spec.AccessURL,
		XrayURL:        spec.XrayURL,
		AccessToken:    string(creds.Data[orDefault(spec.Credentials.AccessTokenKey, "accessToken")]),
	}
	if cfg.AccessToken == "" {
		cfg.Username = string(creds.Data[orDefault(spec.Credentials.UsernameKey, "username")])
		cfg.Password = string(creds.Data[orDefault(spec.Credentials.PasswordKey, "password")])
	}

	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if t := spec.TLS; t != nil {
		tlsCfg.InsecureSkipVerify = t.InsecureSkipVerify
		if t.CASecretRef != nil {
			ns := secretNS
			if crossNamespace && t.CASecretRef.Namespace != "" {
				ns = t.CASecretRef.Namespace
			}
			caSecret, err := r.getSecret(ctx, ns, t.CASecretRef.Name)
			if err != nil {
				return nil, err
			}
			key := orDefault(t.CASecretRef.Key, "ca.crt")
			pem := caSecret.Data[key]
			if len(pem) == 0 {
				return nil, fmt.Errorf("secret %s/%s has no key %q", ns, t.CASecretRef.Name, key)
			}
			pool, err := x509.SystemCertPool()
			if err != nil {
				pool = x509.NewCertPool()
			}
			if !pool.AppendCertsFromPEM(pem) {
				return nil, fmt.Errorf("secret %s/%s key %q holds no valid PEM certificates", ns, t.CASecretRef.Name, key)
			}
			tlsCfg.RootCAs = pool
			fmt.Fprintf(fp, "%s|", caSecret.ResourceVersion)
		}
	}
	fingerprint := hex.EncodeToString(fp.Sum(nil))

	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.cache[cacheID]; ok && c.fingerprint == fingerprint {
		return c.client, nil
	}

	timeout := r.HTTPTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsCfg
	cfg.HTTPClient = &http.Client{Timeout: timeout, Transport: transport}
	c, err := jfrog.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	if old, ok := r.cache[cacheID]; ok {
		old.client.CloseIdleConnections()
	}
	if r.cache == nil {
		r.cache = map[string]cached{}
	}
	r.cache[cacheID] = cached{fingerprint: fingerprint, client: c}
	return c, nil
}

func (r *Resolver) getSecret(ctx context.Context, namespace, name string) (*corev1.Secret, error) {
	if name == "" {
		return nil, errors.New("secret name is empty")
	}
	var s corev1.Secret
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &s); err != nil {
		return nil, fmt.Errorf("getting Secret %s/%s: %w", namespace, name, err)
	}
	return &s, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
