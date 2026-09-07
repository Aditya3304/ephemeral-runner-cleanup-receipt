package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/finalizer"
	"github.com/Aditya3304/ephemeral-runner-cleanup-receipt/internal/proof"
	jcs "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

type Config struct {
	Repository    string                 `json:"repository"`
	ArchiveConfig string                 `json:"archive_config"`
	CosignBinary  string                 `json:"cosign_binary"`
	SigningConfig string                 `json:"signing_config"`
	TrustRoot     string                 `json:"trust_root"`
	TokenFile     string                 `json:"token_file"`
	Policy        finalizer.SignerPolicy `json:"policy"`
}

func strictJSON(b []byte, out any) error {
	canonical, e := jcs.Transform(b)
	if e != nil {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e = d.Decode(out); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON")
	}
	shape, e := proof.Canonical(out)
	if e != nil || !bytes.Equal(shape, canonical) {
		return errors.New("exact JSON shape required")
	}
	return nil
}
func LoadConfig(path string) (Config, error) {
	var c Config
	b, e := proof.ReadBounded(path, 16<<10)
	if e != nil {
		return c, e
	}
	e = strictJSON(b, &c)
	if e == nil && (!identifier.MatchString(c.Repository) || c.Policy.Revision == "") {
		e = errors.New("invalid operator configuration")
	}
	return c, e
}
