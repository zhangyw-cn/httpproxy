package cert

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const (
	caCertFilename = "ca.crt"
	caKeyFilename  = "ca.key"
)

type Manager struct {
	caCert *x509.Certificate
	caKey  crypto.Signer
	caPEM  []byte

	mu     sync.Mutex
	leaves map[string]*tls.Certificate
}

func LoadOrCreate(caDir string) (*Manager, error) {
	if err := os.MkdirAll(caDir, 0o755); err != nil {
		return nil, fmt.Errorf("create CA directory: %w", err)
	}
	unlock, err := lockCAInit(caDir)
	if err != nil {
		return nil, err
	}
	defer unlock()

	certPath := filepath.Join(caDir, caCertFilename)
	keyPath := filepath.Join(caDir, caKeyFilename)
	certPEM, certErr := os.ReadFile(certPath)
	keyPEM, keyErr := os.ReadFile(keyPath)
	switch {
	case certErr == nil && keyErr == nil:
		if err := os.Chmod(keyPath, 0o600); err != nil {
			return nil, fmt.Errorf("secure CA key: %w", err)
		}
	case errors.Is(certErr, os.ErrNotExist) && errors.Is(keyErr, os.ErrNotExist):
		certPEM, keyPEM, err = generateCA()
		if err != nil {
			return nil, err
		}
		if err := publishCA(certPath, keyPath, certPEM, keyPEM); err != nil {
			return nil, err
		}
	case errors.Is(certErr, os.ErrNotExist) != errors.Is(keyErr, os.ErrNotExist):
		return nil, errors.New("incomplete CA files: ca.crt and ca.key must both exist")
	default:
		if certErr != nil {
			return nil, fmt.Errorf("read CA certificate: %w", certErr)
		}
		return nil, fmt.Errorf("read CA key: %w", keyErr)
	}

	caCert, err := parseCertificate(certPEM)
	if err != nil {
		return nil, fmt.Errorf("parse CA certificate: %w", err)
	}
	caKey, err := parsePrivateKey(keyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse CA key: %w", err)
	}
	certPublicKey, err := x509.MarshalPKIXPublicKey(caCert.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal CA certificate public key: %w", err)
	}
	privatePublicKey, err := x509.MarshalPKIXPublicKey(caKey.Public())
	if err != nil {
		return nil, fmt.Errorf("marshal CA private key public key: %w", err)
	}
	if !bytes.Equal(certPublicKey, privatePublicKey) {
		return nil, errors.New("CA certificate public key does not match CA private key")
	}
	if !caCert.IsCA {
		return nil, errors.New("CA certificate is not a certificate authority")
	}

	return &Manager{
		caCert: caCert,
		caKey:  caKey,
		caPEM:  append([]byte(nil), certPEM...),
		leaves: make(map[string]*tls.Certificate),
	}, nil
}

func lockCAInit(caDir string) (func(), error) {
	lock, err := os.OpenFile(filepath.Join(caDir, ".ca.init.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open CA initialization lock: %w", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("lock CA initialization: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}, nil
}

func publishCA(certPath, keyPath string, certPEM, keyPEM []byte) error {
	keyTemp, err := writeTempFile(filepath.Dir(keyPath), ".ca.key-*.tmp", keyPEM, 0o600)
	if err != nil {
		return fmt.Errorf("stage CA key: %w", err)
	}
	defer os.Remove(keyTemp)

	certTemp, err := writeTempFile(filepath.Dir(certPath), ".ca.crt-*.tmp", certPEM, 0o644)
	if err != nil {
		return fmt.Errorf("stage CA certificate: %w", err)
	}
	defer os.Remove(certTemp)

	if err := os.Rename(keyTemp, keyPath); err != nil {
		return fmt.Errorf("publish CA key: %w", err)
	}
	if err := os.Rename(certTemp, certPath); err != nil {
		_ = os.Remove(keyPath)
		return fmt.Errorf("publish CA certificate: %w", err)
	}
	return nil
}

func writeTempFile(dir, pattern string, data []byte, mode os.FileMode) (path string, err error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	path = file.Name()
	defer func() {
		if err != nil {
			_ = file.Close()
			_ = os.Remove(path)
		}
	}()
	if err = file.Chmod(mode); err != nil {
		return "", err
	}
	if _, err = file.Write(data); err != nil {
		return "", err
	}
	if err = file.Sync(); err != nil {
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	return path, nil
}

func (m *Manager) CACertPEM() []byte {
	return append([]byte(nil), m.caPEM...)
}

func (m *Manager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	host := "localhost"
	if hello != nil && hello.ServerName != "" {
		host = hello.ServerName
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if certificate := m.leaves[host]; certificate != nil {
		return certificate, nil
	}

	certificate, err := m.createLeaf(host)
	if err != nil {
		return nil, err
	}
	m.leaves[host] = certificate
	return certificate, nil
}

func (m *Manager) createLeaf(host string) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate leaf key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, fmt.Errorf("generate leaf serial: %w", err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, m.caCert, &key.PublicKey, m.caKey)
	if err != nil {
		return nil, fmt.Errorf("create leaf certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal leaf key: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("build leaf key pair: %w", err)
	}
	return &certificate, nil
}

func generateCA() ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate CA key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, fmt.Errorf("generate CA serial: %w", err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "httpproxy Local CA"},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.AddDate(10, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create CA certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal CA key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, limit)
}

func parseCertificate(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("missing CERTIFICATE PEM block")
	}
	return x509.ParseCertificate(block.Bytes)
}

func parsePrivateKey(data []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("missing private key PEM block")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		signer, ok := key.(crypto.Signer)
		if !ok {
			return nil, errors.New("private key cannot sign")
		}
		return signer, nil
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, errors.New("unsupported private key format")
}
