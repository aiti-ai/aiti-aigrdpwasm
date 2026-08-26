package nla

import (
	"encoding/asn1"
	"log/slog"
)

type NegoToken struct {
	Data []byte `asn1:"explicit,tag:0"`
}

type TSRequest struct {
	Version     int         `asn1:"explicit,tag:0"`
	NegoTokens  []NegoToken `asn1:"optional,explicit,tag:1"`
	AuthInfo    []byte      `asn1:"optional,explicit,tag:2"`
	PubKeyAuth  []byte      `asn1:"optional,explicit,tag:3"`
	//ErrorCode   int         `asn1:"optional,explicit,tag:4"`
	ClientNonce []byte      `asn1:"optional,explicit,tag:5"` // CVE-2018-0886 hardening: 32-byte nonce, mandatory for version 6
}

type TSCredentials struct {
	CredType    int    `asn1:"explicit,tag:0"`
	Credentials []byte `asn1:"explicit,tag:1"` // [1] EXPLICIT OCTET STRING (DER of the TSPasswordCreds SEQUENCE, per MS-CSSP)
}

type TSPasswordCreds struct {
	DomainName []byte `asn1:"explicit,tag:0"`
	UserName   []byte `asn1:"explicit,tag:1"`
	Password   []byte `asn1:"explicit,tag:2"`
}

type TSCspDataDetail struct {
	KeySpec       int    `asn1:"explicit,tag:0"`
	CardName      string `asn1:"explicit,tag:1"`
	ReaderName    string `asn1:"explicit,tag:2"`
	ContainerName string `asn1:"explicit,tag:3"`
	CspName       string `asn1:"explicit,tag:4"`
}

type TSSmartCardCreds struct {
	Pin        string            `asn1:"explicit,tag:0"`
	CspData    []TSCspDataDetail `asn1:"explicit,tag:1"`
	UserHint   string            `asn1:"explicit,tag:2"`
	DomainHint string            `asn1:"explicit,tag:3"`
}

func EncodeDERTRequest(msgs []Message, authInfo []byte, pubKeyAuth []byte, clientNonce []byte) []byte {
	req := TSRequest{
		Version: 6, // hardened Windows wants version 6 + clientNonce (same as FreeRDP); version 2 gets rejected with SEC_E_INTERNAL_ERROR at the authInfo stage
	}

	if len(msgs) > 0 {
		req.NegoTokens = make([]NegoToken, 0, len(msgs))
	}

	for _, msg := range msgs {
		token := NegoToken{msg.Serialize()}
		req.NegoTokens = append(req.NegoTokens, token)
	}

	if len(authInfo) > 0 {
		req.AuthInfo = authInfo
	}

	if len(pubKeyAuth) > 0 {
		req.PubKeyAuth = pubKeyAuth
	}

	if len(clientNonce) > 0 {
		req.ClientNonce = clientNonce
	}

	result, err := asn1.Marshal(req)
	if err != nil {
		slog.Error("EncodeDERTRequest", "err", err)
	}
	return result
}

func DecodeDERTRequest(s []byte) (*TSRequest, error) {
	treq := &TSRequest{}
	_, err := asn1.Unmarshal(s, treq)
	return treq, err
}
func EncodeDERTCredentials(domain, username, password []byte) []byte {
	// MS-CSSP: TSPasswordCreds strings are UTF-16LE and must be null-terminated (same behavior as FreeRDP)
	nulTerm := func(b []byte) []byte {
		if len(b) >= 2 && b[len(b)-2] == 0 && b[len(b)-1] == 0 {
			return b
		}
		return append(b, 0x00, 0x00)
	}
	tpas := TSPasswordCreds{nulTerm(domain), nulTerm(username), nulTerm(password)}
	result, err := asn1.Marshal(tpas)
	if err != nil {
		slog.Error("EncodeDERTCredentials", "err", err)
	}
	// credentials field: [1] EXPLICIT OCTET STRING wrapping the DER of the
	// TSPasswordCreds SEQUENCE (per MS-CSSP; FreeRDP
	// WinPrAsn1DecReadContextualOctetString also reads an OCTET STRING first and
	// then decodes the SEQUENCE inside)
	tcre := TSCredentials{
		CredType:    1,
		Credentials: result,
	}
	result, err = asn1.Marshal(tcre)
	if err != nil {
		slog.Error("EncodeDERTCredentials", "err", err)
	}
	return result
}

func DecodeDERTCredentials(s []byte) (*TSCredentials, error) {
	tcre := &TSCredentials{}
	_, err := asn1.Unmarshal(s, tcre)
	return tcre, err
}
