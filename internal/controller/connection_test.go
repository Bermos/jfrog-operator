package controller_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/Bermos/jfrog-operator/api/common/v1alpha1"
	platformv1alpha1 "github.com/Bermos/jfrog-operator/api/platform/v1alpha1"
	"github.com/Bermos/jfrog-operator/internal/jfrog/fake"
)

func TestClusterConnectionReady(t *testing.T) {
	conn := &platformv1alpha1.ClusterConnection{ObjectMeta: metav1.ObjectMeta{Name: commonv1alpha1.DefaultConnectionName}}
	eventually(t, func() error {
		if err := get(t, conn); err != nil {
			return err
		}
		if err := conditionIs(conn.Status.Conditions, commonv1alpha1.ConditionReady, metav1.ConditionTrue, commonv1alpha1.ReasonAvailable); err != nil {
			return err
		}
		if conn.Status.ArtifactoryVersion != "7.104.5" || conn.Status.XrayVersion != "3.111.0" {
			return fmt.Errorf("versions not reported: %+v", conn.Status)
		}
		return nil
	})
}

func TestConnectionWithBadCredentials(t *testing.T) {
	ctx := context.Background()
	ns := newNamespace(t)
	mustCreate(t, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: ns},
		StringData: map[string]string{"accessToken": "wrong"},
	})
	conn := &platformv1alpha1.Connection{
		ObjectMeta: metav1.ObjectMeta{Name: "bad", Namespace: ns},
		Spec: platformv1alpha1.ConnectionSpec{
			URL:         jfrogs.URL,
			Credentials: platformv1alpha1.Credentials{SecretRef: platformv1alpha1.SecretReference{Name: "creds"}},
		},
	}
	mustCreate(t, conn)
	eventually(t, func() error {
		if err := get(t, conn); err != nil {
			return err
		}
		return conditionIs(conn.Status.Conditions, commonv1alpha1.ConditionReady, metav1.ConditionFalse, commonv1alpha1.ReasonUnavailable)
	})

	// Fixing the Secret makes the connection ready on the next check.
	var secret corev1.Secret
	if err := k8s.Get(ctx, keyOf(ns, "creds"), &secret); err != nil {
		t.Fatal(err)
	}
	secret.Data["accessToken"] = []byte(fake.Token)
	if err := k8s.Update(ctx, &secret); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() error {
		if err := get(t, conn); err != nil {
			return err
		}
		return conditionIs(conn.Status.Conditions, commonv1alpha1.ConditionReady, metav1.ConditionTrue, commonv1alpha1.ReasonAvailable)
	})
}

func TestConnectionWithMissingSecret(t *testing.T) {
	ns := newNamespace(t)
	conn := &platformv1alpha1.Connection{
		ObjectMeta: metav1.ObjectMeta{Name: "nosecret", Namespace: ns},
		Spec: platformv1alpha1.ConnectionSpec{
			URL:         jfrogs.URL,
			Credentials: platformv1alpha1.Credentials{SecretRef: platformv1alpha1.SecretReference{Name: "missing"}},
		},
	}
	mustCreate(t, conn)
	eventually(t, func() error {
		if err := get(t, conn); err != nil {
			return err
		}
		return conditionIs(conn.Status.Conditions, commonv1alpha1.ConditionReady, metav1.ConditionFalse, commonv1alpha1.ReasonConnectionError)
	})
}

func TestNamespacedConnectionIgnoresSecretNamespace(t *testing.T) {
	// A namespaced Connection must not be able to read Secrets of other
	// namespaces, here the operator's own credentials.
	ns := newNamespace(t)
	conn := &platformv1alpha1.Connection{
		ObjectMeta: metav1.ObjectMeta{Name: "steal", Namespace: ns},
		Spec: platformv1alpha1.ConnectionSpec{
			URL: jfrogs.URL,
			Credentials: platformv1alpha1.Credentials{
				SecretRef: platformv1alpha1.SecretReference{Name: "jfrog-credentials", Namespace: operatorNS},
			},
		},
	}
	mustCreate(t, conn)
	eventually(t, func() error {
		if err := get(t, conn); err != nil {
			return err
		}
		if err := conditionIs(conn.Status.Conditions, commonv1alpha1.ConditionReady, metav1.ConditionFalse, commonv1alpha1.ReasonConnectionError); err != nil {
			return err
		}
		if msg := conn.Status.Conditions[0].Message; !strings.Contains(msg, ns+"/jfrog-credentials") {
			return fmt.Errorf("secret should be looked up in the connection's namespace: %s", msg)
		}
		return nil
	})
}

func TestClusterConnectionRequiresSecretNamespace(t *testing.T) {
	conn := &platformv1alpha1.ClusterConnection{
		ObjectMeta: metav1.ObjectMeta{Name: "no-namespace"},
		Spec: platformv1alpha1.ConnectionSpec{
			URL:         jfrogs.URL,
			Credentials: platformv1alpha1.Credentials{SecretRef: platformv1alpha1.SecretReference{Name: "x"}},
		},
	}
	err := k8s.Create(context.Background(), conn)
	if err == nil || !strings.Contains(err.Error(), "namespace is required") {
		t.Fatalf("want validation error, got %v", err)
	}
}

func TestConnectionURLValidation(t *testing.T) {
	conn := &platformv1alpha1.Connection{
		ObjectMeta: metav1.ObjectMeta{Name: "badurl", Namespace: "default"},
		Spec: platformv1alpha1.ConnectionSpec{
			URL:         "jfrog.example.com",
			Credentials: platformv1alpha1.Credentials{SecretRef: platformv1alpha1.SecretReference{Name: "x"}},
		},
	}
	if err := k8s.Create(context.Background(), conn); err == nil {
		t.Fatal("a URL without scheme must be rejected")
	}
}
