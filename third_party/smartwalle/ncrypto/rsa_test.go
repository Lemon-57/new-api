package ncrypto_test

import (
	"encoding/hex"
	"github.com/smartwalle/ncrypto"
	"testing"
)

func TestNewRSAKeyPair(t *testing.T) {
	var private, public, err = ncrypto.GenerateRSAKeyPair(1024)
	if err != nil {
		t.Fatal(err)
	}
	_ = private
	_ = public
	//
	//if _, err = pkcs8.DecodeRSAPrivateKey(private); err != nil {
	//	t.Fatal(err)
	//}
	//
	//if _, err = pkix.DecodeRSAPublicKey(public); err != nil {
	//	t.Fatal(err)
	//}
}

func TestRSAEncryptWithPKCS1(t *testing.T) {
	var pub = []byte(`-----BEGIN RSA PUBLIC KEY-----
MIGJAoGBALGEKwV7SfAmCAS3/ZQtApqrvBniYmfBxpnvWLjlfYxUMQ5Ve5QmNDdH
LcSNKaVfql3EQVA1ClQH6wf4eZprg43NdWfKNNjUz+UITd3Ob3sV+xqWchNTcchU
zq9nPFn9wxatbcjKfyGWBYYwO4Cv1oqdVbPmyPNhJicbY7/JtNnVAgMBAAE=
-----END RSA PUBLIC KEY-----`)

	var pri = []byte(`-----BEGIN RSA PRIVATE KEY-----
MIICXAIBAAKBgQCxhCsFe0nwJggEt/2ULQKaq7wZ4mJnwcaZ71i45X2MVDEOVXuU
JjQ3Ry3EjSmlX6pdxEFQNQpUB+sH+Hmaa4ONzXVnyjTY1M/lCE3dzm97FfsalnIT
U3HIVM6vZzxZ/cMWrW3Iyn8hlgWGMDuAr9aKnVWz5sjzYSYnG2O/ybTZ1QIDAQAB
AoGBAKDedJpYGy49WODl2DBzBbwjS6htZt4+VftkUxPkLP2Bwp8Jyp78bC94GrrX
bllGs76bvtCv8HcsYcrsW08cha72WC+FhN09XeloCqe8so9ogjecUcyebU9MrVV9
Bk+TljXfU9IAScyyT39GNxUP6Vrp46YndCut4oyEBviwyK31AkEA3NAsbSVyaTFk
ytxeU99xqiPDb6rdZMew3B+mle9brXtk19c+v5/hIocXBAScdOr2Zvq/UfstqBl8
PWH1bAe7owJBAM3Nvv8OkbewUR5lKH9Q1SuKPDRqIKIvW6hwv4n38SVNQwHCGmtf
4zqx1nkdc0NEiKaWZ4xbuPJP+Q9dDUcP7CcCQEfRARICouJpqTF9WMSIoMxIU3EH
LnhvpisBtEmBjtyujE7S99qVIkD7lNW+tjAklz1JAl6kheXtXAYOzSZ0oWcCQDRy
4OAjmGNV3fZ/FUkNEqab/iflfBeZNiTBRy8kLyKwhAkorI78yu4kcGJBbSSRjLnX
zt/oaEPoubJ+pmmb1zUCQDLHe9PJNVdx/Z++ImB+mT78yIn3aSUMyHYTe1KVwWop
5nRL12LwFytOAMwC8LEg3VDHDpxd9+O+pfd+Coum1OM=
-----END RSA PRIVATE KEY-----`)

	publicKey, err := ncrypto.DecodePublicKey(pub).PKCS1().RSAPublicKey()
	if err != nil {
		t.Fatal(err)
	}

	privateKey, err := ncrypto.DecodePrivateKey(pri).PKCS1().RSAPrivateKey()
	if err != nil {
		t.Fatal(err)
	}

	ciphertext, err := ncrypto.RSAEncrypt([]byte("Sent when the application is about to move from active to inactive state. This can occur for certain types of temporary interruptions (such as an incoming phone call or SMS message) or when the user quits the application and it begins the transition to the background state."), publicKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(hex.EncodeToString(ciphertext))

	plaintext, err := ncrypto.RSADecrypt(ciphertext, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(plaintext))
}

func TestRSAEncryptWithPKCS8(t *testing.T) {
	var pub = []byte(`-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQCc5dZdXoHFPnibqjfVvxKXcVM3
WQGj2qdzRNE7hxkyne7x6XaYjGJpeIG1iIwaQFWisJy6Z4aelkjZEGRF9lA7uK15
K1+eMuY6ryYKwt+ViUi1mMFnw6SiyjrZGICEiS5vT/Evdks7lVK5389MiVF2Dfed
DolwAvAZ6IGgOH509QIDAQAB
-----END PUBLIC KEY-----`)

	var pri = []byte(`-----BEGIN PRIVATE KEY-----
MIICdwIBADANBgkqhkiG9w0BAQEFAASCAmEwggJdAgEAAoGBAJzl1l1egcU+eJuq
N9W/EpdxUzdZAaPap3NE0TuHGTKd7vHpdpiMYml4gbWIjBpAVaKwnLpnhp6WSNkQ
ZEX2UDu4rXkrX54y5jqvJgrC35WJSLWYwWfDpKLKOtkYgISJLm9P8S92SzuVUrnf
z0yJUXYN950OiXAC8BnogaA4fnT1AgMBAAECgYEAgE0StteJlxo21lSjxA6zzVPG
kQQf6zXqqMAluWAIovOzae9YI/boowcASsqWhwEFBj0WbPgrhZOvjpFw7iU2BdhW
XkekaRgwc4d2rOzLNfF+0f0R9Z5XkoIjP6lhY/vFF2YQEqj6H83lP0B6yIsDT35K
lSAIc8yfVm2XE2VguQECQQDO2vxBWsRbdZmw3TT/QTri4x9k2xJrjnDcMOItEH5y
QixCaiEeDBKMNLqG1tu+8fNwkf7CImP4xS0Ap0CWHbpJAkEAwixp5IqZRmhu5PAK
mTMcVm3TD2LJIhCJA0wHccuZRIvLAWqBmFkatHxc8jOXjPtgNsfaF4ZnKE3Wy0fH
BZoFTQJBAJ3Y6UmN0+zezoryIjDuO9tK6Xfy3BmLNoAwJUeyIGtcJ53+kor1N2oa
CoQ+jK0mwFeUcMz/pT3+aJrpBhYHVVECQFSVzmdNbSm7spsuah+EoVKRMwEf0mhx
dY4nH5MV0yWGFCAAyoWYQ0beagrkKJ+0nZwfgUUAOo3XIruY//zTtH0CQAWPUETF
w8O7v/wsAIadDyGYewgy7DT2wBtWCc4xEGwV87UIAKf7MikwKSsTKT3SQoMZnojC
cW0QvhodYyxnlSs=
-----END PRIVATE KEY-----`)

	publicKey, err := ncrypto.DecodePublicKey(pub).PKIX().RSAPublicKey()
	if err != nil {
		t.Fatal(err)
	}

	privateKey, err := ncrypto.DecodePrivateKey(pri).PKCS8().RSAPrivateKey()
	if err != nil {
		t.Fatal(err)
	}

	ciphertext, err := ncrypto.RSAEncrypt([]byte("Sent when the application is about to move from active to inactive state. This can occur for certain types of temporary interruptions (such as an incoming phone call or SMS message) or when the user quits the application and it begins the transition to the background state."), publicKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(hex.EncodeToString(ciphertext))

	plaintext, err := ncrypto.RSADecrypt(ciphertext, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(plaintext))
}
