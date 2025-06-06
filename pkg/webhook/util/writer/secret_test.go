package writer

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientsetfake "k8s.io/client-go/kubernetes/fake"
)

// Test that EnsureCert writes certs to a secret
func TestSecretCertWriterEnsureCert(t *testing.T) {
	cli := clientsetfake.NewSimpleClientset()
	sec := types.NamespacedName{Name: "webhook-server-cert", Namespace: "default"}
	w, err := NewSecretCertWriter(SecretCertWriterOptions{Clientset: cli, Secret: &sec})
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	certs, created, err := w.EnsureCert("test.example.com")
	if err != nil || !created {
		t.Fatalf("ensure cert: %v created=%v", err, created)
	}
	s, err := cli.CoreV1().Secrets("default").Get(context.TODO(), sec.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("secret not created: %v", err)
	}
	if len(s.Data) == 0 || len(certs.Cert) == 0 {
		t.Fatalf("cert data empty")
	}
}
