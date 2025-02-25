package stacks

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"gin-web/api/v1/wallet"
	ec "github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"math/big"
	"strconv"
	"strings"
)

func addressFromVersionHash(version uint64, hash string) *wallet.Signer {
	signer := &wallet.Signer{}
	signer.Type_ = 0
	signer.Version = version
	signer.Hash160 = hash
	return signer
}

func nextSignature(curSigHash string, authType int, fee, nonce *big.Int, stacksPrivateKey *wallet.StacksPrivateKey) (wallet.NextSignature, error) {
	sigHashPreSign, err := makeSigHashPreSign(curSigHash, authType, fee, nonce)
	if err != nil {
		return wallet.NextSignature{}, err
	}
	signature, err := signWithKey(*stacksPrivateKey, sigHashPreSign)
	if err != nil {
		return wallet.NextSignature{}, err
	}
	privateKey := hex.EncodeToString(stacksPrivateKey.Data)
	publicKey, err := wallet.pubKeyFromPrivKey(privateKey)
	if err != nil {
		return wallet.NextSignature{}, err
	}
	publicKeyEncoding := 1 // uncompressed public key
	if wallet.isCompressed(*publicKey) {
		publicKeyEncoding = 0 // compressed public key
	}

	nextSigHash, err := makeSigHashPostSign(sigHashPreSign, strconv.Itoa(publicKeyEncoding), signature)
	if err != nil {
		return wallet.NextSignature{}, err
	}
	return wallet.NextSignature{signature, nextSigHash}, nil

}

func makeSigHashPostSign(curSigHash string, pubKeyEncoding string, signature wallet.MessageSignature) (string, error) {
	hashLength := 98
	s := hex.EncodeToString(wallet.fromHexString(pubKeyEncoding))
	sigHash := curSigHash + wallet.leftPadHex(s) + signature.Data
	b := wallet.fromHexString(sigHash)
	if len(b) > hashLength {
		return "", errors.New("Invalid signature hash length")
	}
	return wallet.txidFromData(b), nil
}

func bytesToHexString(b []byte) string {
	var buf bytes.Buffer
	for _, v := range b {
		t := strconv.FormatInt(int64(v), 16)
		if len(t) > 1 {
			buf.WriteString(t)
		} else {
			buf.WriteString("0" + t)
		}
	}
	return buf.String()
}

func BigToHex(in *big.Int) string {
	return fmt.Sprintf("%x", in)
}

func signWithKey(privateKey wallet.StacksPrivateKey, input string) (wallet.MessageSignature, error) {
	substring := hex.EncodeToString(privateKey.Data)[:64]
	signature, v, err := sign(substring, input)
	if err != nil {
		return wallet.MessageSignature{}, err
	}
	var parsedSignature struct {
		R, S *big.Int
	}
	_, err = asn1.Unmarshal(signature.Serialize(), &parsedSignature)
	if err != nil {
		return wallet.MessageSignature{}, err
	}
	coordinateValueBytes := 32
	r := strings.Repeat("0", coordinateValueBytes*2-len(parsedSignature.R.Text(16))) + parsedSignature.R.Text(16)
	s := strings.Repeat("0", coordinateValueBytes*2-len(parsedSignature.S.Text(16))) + parsedSignature.S.Text(16)
	result := int(*v)
	length := 1
	recoveryParam := wallet.intToHexString(result, &length)
	recoverableSignatureString := recoveryParam + r + s
	recoverableSignature := createMessageSignature(recoverableSignatureString)
	return recoverableSignature, nil
}

func createMessageSignature(signature string) wallet.MessageSignature {
	length := len(wallet.fromHexString(signature))
	if length != 65 {
		panic("Invalid signature")
	}
	messageSignature := wallet.MessageSignature{}
	messageSignature.Type_ = 9
	messageSignature.Data = signature
	return messageSignature
}

func sign(privateKey, txHex string) (*ec.Signature, *uint8, error) {
	privKeyBytes, err := hex.DecodeString(privateKey)
	if err != nil {
		return nil, nil, err
	}

	privKey, _ := btcec.PrivKeyFromBytes(privKeyBytes)

	txBytes, err := hex.DecodeString(txHex)
	if err != nil {
		return nil, nil, err
	}
	sig := ec.Sign(privKey, txBytes)

	sig2, err := ec.SignCompact(privKey, txBytes, false)
	if err != nil {
		return nil, nil, err
	}
	v := sig2[0] - 27
	copy(sig2, sig2[1:])
	sig2[64] = v
	return sig, &v, nil
}

func makeSigHashPreSign(curSigHash string, authType int, fee, nonce *big.Int) (string, error) {
	hashLength := 49
	authTypeBytes := []byte{byte(authType)}
	feeBytes := wallet.toArrayLike(fee, 8)
	nonceBytes := wallet.toArrayLike(nonce, 8)
	sigHashBytes, err := hex.DecodeString(curSigHash + hex.EncodeToString(authTypeBytes) + hex.EncodeToString(feeBytes) + hex.EncodeToString(nonceBytes))
	if err != nil {
		return "", err
	}
	if len(sigHashBytes) != hashLength {
		panic("Invalid signature hash length")
	}
	return wallet.txidFromData(sigHashBytes), nil
}

func signStacksTransfer(privateKeyHex string, transferSig wallet.StacksTransferSig) ([]byte, error) {
	// Decoding the private key
	privateKeyBytes, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return nil, err
	}
	if len(privateKeyBytes) != wallet.privatekeybytes1 {
		return nil, errors.New("invalid private key length")
	}
	privateKey := new(ecdsa.PrivateKey)
	privateKey.Curve = btcec.S256()
	privateKey.D = new(big.Int).SetBytes(privateKeyBytes)
	// calculate message hash
	transferSigBytes, err := json.Marshal(transferSig)
	if err != nil {
		return nil, err
	}
	messageHash := sha256.Sum256(transferSigBytes)
	// sign
	r, s, err := ecdsa.Sign(rand.Reader, privateKey, messageHash[:])
	if err != nil {
		return nil, err
	}
	signature := make([]byte, wallet.signatureBytes)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(signature[32-len(rBytes):32], rBytes)
	copy(signature[64-len(sBytes):64], sBytes)
	return signature, nil
}
func SignStacksTransfer(privateKeyHex string, transferSig wallet.StacksTransferSig) ([]byte, error) {
	// Decoding the private key
	privateKeyBytes, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return nil, err
	}
	if len(privateKeyBytes) != wallet.privatekeybytes1 {
		return nil, errors.New("invalid private key length")
	}
	privateKey := new(ecdsa.PrivateKey)
	privateKey.Curve = btcec.S256()
	privateKey.D = new(big.Int).SetBytes(privateKeyBytes)
	// calculate message hash
	transferSigBytes, err := json.Marshal(transferSig)
	if err != nil {
		return nil, err
	}
	messageHash := sha256.Sum256(transferSigBytes)
	// sign
	r, s, err := ecdsa.Sign(rand.Reader, privateKey, messageHash[:])
	if err != nil {
		return nil, err
	}
	signature := make([]byte, wallet.signatureBytes)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(signature[32-len(rBytes):32], rBytes)
	copy(signature[64-len(sBytes):64], sBytes)
	return signature, nil
}
