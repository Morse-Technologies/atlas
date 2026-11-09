// Command licensegen makes key pairs and signed Pro licenses for development and tests. The private key it
// writes must never reach a server: a real installation only holds Morse's public key.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/morse/atlas/server/internal/platform/license"
)

const (
	privateKeyFile = "private.key"
	publicKeyFile  = "public.key"
	secretMode     = 0o600
	dirMode        = 0o700
	hoursPerDay    = 24
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "licensegen:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: licensegen keygen --dir D | sign --key F --org NAME --days N --features a,b --out F")
	}
	switch args[0] {
	case "keygen":
		return keygen(args[1:])
	case "sign":
		return sign(args[1:])
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func keygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	dir := fs.String("dir", ".", "where to write private.key and public.key")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}
	if err := os.MkdirAll(*dir, dirMode); err != nil {
		return err
	}
	if err := writeKey(filepath.Join(*dir, privateKeyFile), priv); err != nil {
		return err
	}
	return writeKey(filepath.Join(*dir, publicKeyFile), pub)
}

func writeKey(path string, key []byte) error {
	return os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(key)), secretMode)
}

type signFlags struct {
	key, org, features, out string
	days                    int
}

func parseSign(args []string) (signFlags, error) {
	var f signFlags
	fs := flag.NewFlagSet("sign", flag.ContinueOnError)
	fs.StringVar(&f.key, "key", "", "private key file made by keygen")
	fs.StringVar(&f.org, "org", "", "organization the license is for")
	fs.StringVar(&f.features, "features", "", "comma separated features, e.g. alerts,map,audit_reports")
	fs.StringVar(&f.out, "out", "", "license file to write")
	fs.IntVar(&f.days, "days", 0, "days the license is valid from today; negative gives an expired one")
	if err := fs.Parse(args); err != nil {
		return f, err
	}
	if f.key == "" || f.org == "" || f.out == "" {
		return f, errors.New("--key, --org and --out are required")
	}
	return f, nil
}

func sign(args []string) error {
	f, err := parseSign(args)
	if err != nil {
		return err
	}
	priv, err := readPrivateKey(f.key)
	if err != nil {
		return err
	}
	until := time.Now().Add(time.Duration(f.days) * hoursPerDay * time.Hour)
	raw, err := license.Sign(priv, license.Grant{Organization: f.org, ValidUntil: until, Features: strings.Split(f.features, ",")})
	if err != nil {
		return err
	}
	return os.WriteFile(f.out, raw, secretMode)
}

func readPrivateKey(path string) (ed25519.PrivateKey, error) {
	text, err := os.ReadFile(path) //nolint:gosec // the path is the operator's own argument
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(text)))
	if err != nil || len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("private key file is not a key made by licensegen keygen")
	}
	return key, nil
}
