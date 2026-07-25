// OpenSSL is a robust, commercial-grade toolkit for SSL/TLS protocols
//
// OpenSSL is a software library for applications that secure communications over
// computer networks against eavesdropping or need to identify the party at the
// other end. It is widely used in Internet servers, including most HTTPS websites.
// OpenSSL contains an open-source implementation of the SSL and TLS protocols and
// provides cryptographic functions for encryption, decryption, and digital signatures.
//
// References:
// - OpenSSL Documentation: https://www.openssl.org/docs/
// - OpenSSL GitHub: https://github.com/openssl/openssl
// - OpenSSL Wiki: https://wiki.openssl.org/
//
// Common openssl commands available through ExecuteCommand():
//   - openssl version - Display version information
//   - openssl version -a - Display all version information
//   - openssl list - List available algorithms and ciphers
//   - openssl genrsa - Generate RSA private key
//   - openssl req - Create certificate requests
//   - openssl x509 - Certificate display and signing
//   - openssl enc - Symmetric cipher operations
//   - openssl dgst - Message digest operations
//   - openssl rand - Generate pseudo-random bytes
//   - openssl s_client - SSL/TLS client program

package openssl

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
)

type OpenSSL struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *OpenSSL {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &OpenSSL{Cmd: osCmd, Base: baseCmd}
}

func (o *OpenSSL) Install() error {
	return o.Cmd.InstallPackage(constants.OpenSSL)
}

func (o *OpenSSL) SoftInstall() error {
	return o.Cmd.MaybeInstallPackage(constants.OpenSSL)
}

func (o *OpenSSL) ForceInstall() error {
	err := o.Uninstall()
	if err != nil {
		return fmt.Errorf("failed to uninstall openssl: %w", err)
	}
	return o.Install()
}

func (o *OpenSSL) Uninstall() error {
	return fmt.Errorf("openssl uninstall not supported through devgeta")
}

func (o *OpenSSL) ForceConfigure() error {
	// openssl typically doesn't require separate configuration files
	// Configuration is usually handled via command-line arguments or openssl.cnf in system paths
	return nil
}

func (o *OpenSSL) SoftConfigure() error {
	// openssl typically doesn't require separate configuration files
	// Configuration is usually handled via command-line arguments or openssl.cnf in system paths
	return nil
}

func (o *OpenSSL) ExecuteCommand(args ...string) error {
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: constants.OpenSSL,
		Args:    args,
	}
	if _, _, err := o.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to run openssl command: %w", err)
	}
	return nil
}

func (o *OpenSSL) Update() error {
	return fmt.Errorf("openssl update not implemented through devgeta")
}
