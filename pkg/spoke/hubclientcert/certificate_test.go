package hubclientcert

import (
	"crypto"
	"crypto/rand"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math"
	"math/big"
	"testing"
	"time"

	certificates "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	certutil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/keyutil"
)

func newCertKey(commonName string, duration time.Duration) ([]byte, []byte, error) {
	signingKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	signingCert, err := certutil.NewSelfSignedCACert(certutil.Config{CommonName: "open-cluster-management.io"}, signingKey)
	if err != nil {
		return nil, nil, err
	}

	key, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	cert, err := newSignedCert(
		certutil.Config{
			CommonName: commonName,
			Usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		},
		key, signingCert, signingKey, duration,
	)
	if err != nil {
		return nil, nil, err
	}

	return encodePrivateKeyPEM(key), encodeCertPEM(cert), nil
}

// encodePrivateKeyPEM returns PEM-encoded private key data
func encodePrivateKeyPEM(key *rsa.PrivateKey) []byte {
	block := pem.Block{
		Type:  keyutil.RSAPrivateKeyBlockType,
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}
	return pem.EncodeToMemory(&block)
}

// encodeCertPEM returns PEM-encoded certificate data
func encodeCertPEM(cert *x509.Certificate) []byte {
	block := pem.Block{
		Type:  certutil.CertificateBlockType,
		Bytes: cert.Raw,
	}
	return pem.EncodeToMemory(&block)
}

// newSignedCert creates a signed certificate using the given CA certificate and key
func newSignedCert(cfg certutil.Config, key crypto.Signer, caCert *x509.Certificate, caKey crypto.Signer, duration time.Duration) (*x509.Certificate, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).SetInt64(math.MaxInt64))
	if err != nil {
		return nil, err
	}
	if len(cfg.CommonName) == 0 {
		return nil, errors.New("must specify a CommonName")
	}
	if len(cfg.Usages) == 0 {
		return nil, errors.New("must specify at least one ExtKeyUsage")
	}

	certTmpl := x509.Certificate{
		Subject: pkix.Name{
			CommonName:   cfg.CommonName,
			Organization: cfg.Organization,
		},
		DNSNames:     cfg.AltNames.DNSNames,
		IPAddresses:  cfg.AltNames.IPs,
		SerialNumber: serial,
		NotBefore:    caCert.NotBefore,
		NotAfter:     time.Now().Add(duration).UTC(),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  cfg.Usages,
	}
	certDERBytes, err := x509.CreateCertificate(cryptorand.Reader, &certTmpl, caCert, key.Public(), caKey)
	if err != nil {
		return nil, err
	}
	return x509.ParseCertificate(certDERBytes)
}

func newKubeconfig(key, cert []byte) clientcmdapi.Config {
	var clientKey, clientCertificate string
	var clientKeyData, clientCertificateData []byte
	if key != nil {
		clientKeyData = key
	} else {
		clientKey = "tls.key"
	}
	if cert != nil {
		clientCertificateData = cert
	} else {
		clientCertificate = "tls.crt"
	}

	kubeconfig := clientcmdapi.Config{
		// Define a cluster stanza based on the bootstrap kubeconfig.
		Clusters: map[string]*clientcmdapi.Cluster{"default-cluster": {
			Server:                "https://127.0.0.1:6001",
			InsecureSkipTLSVerify: true,
		}},
		// Define auth based on the obtained client cert.
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"default-auth": {
			ClientCertificate:     clientCertificate,
			ClientCertificateData: clientCertificateData,
			ClientKey:             clientKey,
			ClientKeyData:         clientKeyData,
		}},
		// Define a context that connects the auth info and cluster, and set it as the default
		Contexts: map[string]*clientcmdapi.Context{"default-context": {
			Cluster:   "default-cluster",
			AuthInfo:  "default-auth",
			Namespace: "default",
		}},
		CurrentContext: "default-context",
	}

	return kubeconfig
}

func TestIsCSRApprovedWithPendingCSR(t *testing.T) {
	csr := &certificates.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "csr-",
		},
		Spec: certificates.CertificateSigningRequestSpec{},
	}

	if isCSRApproved(csr) {
		t.Error("csr is not approved")
	}
}

func TestIsCSRApprovedWithApprovedCSR(t *testing.T) {
	csr := &certificates.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "csr-",
		},
		Spec: certificates.CertificateSigningRequestSpec{},
		Status: certificates.CertificateSigningRequestStatus{
			Conditions: []certificates.CertificateSigningRequestCondition{
				{
					Type: certificates.CertificateApproved,
				},
			},
		},
	}

	if !isCSRApproved(csr) {
		t.Error("csr is approved")
	}
}

func TestIsCSRApprovedWithDeniedCSR(t *testing.T) {
	csr := &certificates.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "csr-",
		},
		Spec: certificates.CertificateSigningRequestSpec{},
		Status: certificates.CertificateSigningRequestStatus{
			Conditions: []certificates.CertificateSigningRequestCondition{
				{
					Type: certificates.CertificateApproved,
				},
				{
					Type: certificates.CertificateDenied,
				},
			},
		},
	}

	if isCSRApproved(csr) {
		t.Error("csr is denied")
	}
}

func TestGetCertValidityPeriod(t *testing.T) {
	_, cert, err := newCertKey("cluster0", 10*time.Second)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "secret",
		},
		Data: map[string][]byte{
			TLSCertFile: cert,
		},
	}

	notBefore, notAfter, err := getCertValidityPeriod(secret)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if notBefore == nil {
		t.Error("notBefore should not be nil")
	}

	if notAfter == nil {
		t.Error("notAfter should not be nil")
	}
}

func TestIsCertificateValid(t *testing.T) {
	_, cert, err := newCertKey("cluster0", 100*time.Second)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	valid, err := IsCertificateValid(cert)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !valid {
		t.Error("cert is valid")
	}
}

func TestIsCertificateValidWithExpiredCert(t *testing.T) {
	_, cert, err := newCertKey("cluster0", -3*time.Second)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	valid, err := IsCertificateValid(cert)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if valid {
		t.Error("cert is expired")
	}
}

func TestHasValidKubeconfig(t *testing.T) {
	kubeconfigData, err := clientcmd.Write(newKubeconfig(nil, nil))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	key, cert, err := newCertKey("cluster0", 100*time.Second)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "secret",
		},
		Data: map[string][]byte{
			KubeconfigFile: kubeconfigData,
			TLSCertFile:    cert,
			TLSKeyFile:     key,
		},
	}

	if !hasValidKubeconfig(secret) {
		t.Error("kubeconfig is valid")
	}
}

func TestHasValidKubeconfigWithExpiredCert(t *testing.T) {
	kubeconfigData, err := clientcmd.Write(newKubeconfig(nil, nil))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	key, cert, err := newCertKey("cluster0", -3*time.Second)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "secret",
		},
		Data: map[string][]byte{
			KubeconfigFile: kubeconfigData,
			TLSCertFile:    cert,
			TLSKeyFile:     key,
		},
	}

	if hasValidKubeconfig(secret) {
		t.Error("kubeconfig is invalid")
	}
}

func TestHasValidKubeconfigWithoutKey(t *testing.T) {
	kubeconfigData, err := clientcmd.Write(newKubeconfig(nil, nil))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	_, cert, err := newCertKey("cluster0", 100*time.Second)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "secret",
		},
		Data: map[string][]byte{
			KubeconfigFile: kubeconfigData,
			TLSCertFile:    cert,
		},
	}

	if hasValidKubeconfig(secret) {
		t.Error("kubeconfig is invalid")
	}
}

func TestHasValidKubeconfigWithoutCert(t *testing.T) {
	kubeconfigData, err := clientcmd.Write(newKubeconfig(nil, nil))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	key, _, err := newCertKey("cluster0", 100*time.Second)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "secret",
		},
		Data: map[string][]byte{
			KubeconfigFile: kubeconfigData,
			TLSKeyFile:     key,
		},
	}

	if hasValidKubeconfig(secret) {
		t.Error("kubeconfig is invalid")
	}
}

func TestHasValidKubeconfigWithoutKubeconfig(t *testing.T) {
	key, cert, err := newCertKey("cluster0", 100*time.Second)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "secret",
		},
		Data: map[string][]byte{
			TLSCertFile: cert,
			TLSKeyFile:  key,
		},
	}

	if hasValidKubeconfig(secret) {
		t.Error("kubeconfig is invalid")
	}
}
