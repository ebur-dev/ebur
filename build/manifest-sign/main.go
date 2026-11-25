// Command manifest-sign creates the manifest signing key and signs or
// verifies manifest/runtimes.json (SEC-08).
//
//	go run ./build/manifest-sign keygen -out <folder outside the repo>
//	go run ./build/manifest-sign sign -in manifest/runtimes.json -out runtimes.json.sig
//	go run ./build/manifest-sign verify -in manifest/runtimes.json -sig runtimes.json.sig
//
// sign reads the private key (base64 Ed25519 seed) from the environment
// variable MANIFEST_SIGNING_KEY, set from a GitHub Actions secret. The key
// never goes into the repository.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ebur-dev/ebur/core/catalog"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	fs := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	in := fs.String("in", "manifest/runtimes.json", "manifest file")
	sigPath := fs.String("sig", "", "signature file (verify)")
	out := fs.String("out", "", "output: signature file (sign) or folder (keygen)")
	fs.Parse(os.Args[2:])
	switch os.Args[1] {
	case "keygen":
		if *out == "" {
			fail("keygen needs -out <folder outside the repository>")
		}
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			fail(err.Error())
		}
		if err := os.MkdirAll(*out, 0o700); err != nil {
			fail(err.Error())
		}
		keyFile := filepath.Join(*out, "ebur-manifest-signing-key.txt")
		if _, err := os.Stat(keyFile); err == nil {
			fail(keyFile + " already exists; refusing to overwrite a signing key")
		}
		seed := base64.StdEncoding.EncodeToString(priv.Seed())
		if err := os.WriteFile(keyFile, []byte(seed+"\n"), 0o600); err != nil {
			fail(err.Error())
		}
		fmt.Printf("private key written to %s (keep it secret; store it as the MANIFEST_SIGNING_KEY secret)\n", keyFile)
		fmt.Printf("public key: %s\n", base64.StdEncoding.EncodeToString(pub))
	case "sign":
		seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("MANIFEST_SIGNING_KEY")))
		if err != nil || len(seed) != ed25519.SeedSize {
			fail("MANIFEST_SIGNING_KEY is missing or not a base64 Ed25519 seed")
		}
		priv := ed25519.NewKeyFromSeed(seed)
		data, err := os.ReadFile(*in)
		if err != nil {
			fail(err.Error())
		}
		sig := catalog.Sign(priv, data)
		if catalog.Verify(data, sig) != nil {
			fail("the key does not match any public key built into Ebur (core/catalog.PublicKeys)")
		}
		if *out == "" {
			*out = *in + ".sig"
		}
		if err := os.WriteFile(*out, sig, 0o644); err != nil {
			fail(err.Error())
		}
		fmt.Println("signed", *in, "->", *out)
	case "verify":
		data, err := os.ReadFile(*in)
		if err != nil {
			fail(err.Error())
		}
		if *sigPath == "" {
			*sigPath = *in + ".sig"
		}
		sig, err := os.ReadFile(*sigPath)
		if err != nil {
			fail(err.Error())
		}
		if err := catalog.Verify(data, sig); err != nil {
			fail(err.Error())
		}
		fmt.Println("signature OK")
	default:
		usage()
	}
}

func usage() {
	fail("usage: manifest-sign keygen -out <folder> | sign [-in f] [-out f.sig] | verify [-in f] [-sig f.sig]")
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "manifest-sign:", msg)
	os.Exit(1)
}
