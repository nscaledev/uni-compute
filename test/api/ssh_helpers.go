/*
Copyright 2026 Nscale.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package api

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

type SSHKeyPair struct {
	AuthorizedKey    string
	PrivateKeyPEM    string
	PrivateKeySigner ssh.Signer
	PublicKeySSH     ssh.PublicKey
}

func NewSSHKeyPair() (*SSHKeyPair, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating Ed25519 keypair: %w", err)
	}

	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("marshaling private key: %w", err)
	}

	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privateKeyDER,
	})

	privateKeySigner, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("creating SSH signer: %w", err)
	}

	publicKeySSH, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("creating SSH public key: %w", err)
	}

	return &SSHKeyPair{
		AuthorizedKey:    string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(publicKeySSH))),
		PrivateKeyPEM:    string(privateKeyPEM),
		PrivateKeySigner: privateKeySigner,
		PublicKeySSH:     publicKeySSH,
	}, nil
}

func sshCertificateUnixTime(value time.Time) uint64 {
	unix := value.Unix()
	if unix < 0 {
		// The SSH library requires uint64 timestamps on certificates, so keep the
		// signed-to-unsigned conversion explicit and guarded for gosec.
		return 0
	}

	return uint64(unix)
}

func SignSSHUserCertificate(caSigner, userSigner ssh.Signer, principal string, validFor time.Duration) (ssh.AuthMethod, error) {
	certificate := &ssh.Certificate{
		Key:             userSigner.PublicKey(),
		Serial:          1,
		CertType:        ssh.UserCert,
		KeyId:           uuid.NewString(),
		ValidPrincipals: []string{principal},
		ValidAfter:      sshCertificateUnixTime(time.Now().Add(-time.Minute)),
		ValidBefore:     sshCertificateUnixTime(time.Now().Add(validFor)),
		Permissions: ssh.Permissions{
			Extensions: map[string]string{
				"permit-pty": "",
			},
		},
	}

	if err := certificate.SignCert(rand.Reader, caSigner); err != nil {
		return nil, fmt.Errorf("signing SSH certificate: %w", err)
	}

	certificateSigner, err := ssh.NewCertSigner(certificate, userSigner)
	if err != nil {
		return nil, fmt.Errorf("creating SSH certificate signer: %w", err)
	}

	return ssh.PublicKeys(certificateSigner), nil
}

func RunSSHCommand(address, user string, authMethods []ssh.AuthMethod, timeout time.Duration, command string) (string, error) {
	client, err := ssh.Dial("tcp", address, &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // integration tests connect to ephemeral infrastructure
		Timeout:         timeout,
	})
	if err != nil {
		return "", fmt.Errorf("dialing SSH: %w", err)
	}

	defer func() {
		_ = client.Close()
	}()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("creating SSH session: %w", err)
	}

	defer func() {
		_ = session.Close()
	}()

	output, err := session.CombinedOutput(command)
	if err != nil {
		return "", fmt.Errorf("running SSH command %q: %w, output: %s", command, err, string(output))
	}

	return string(bytes.TrimSpace(output)), nil
}

func WaitForSSHReady(publicIP string, timeout time.Duration) {
	WaitForTCPPort(net.JoinHostPort(publicIP, "22"), timeout)
}
