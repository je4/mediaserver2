package mediaserver

import (
	"fmt"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"
)

func NewJWT(secret string, subject string, valid int64) (tokenString string, err error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": strings.ToLower(subject),
		"nbf": time.Now().Unix() + valid,
	})
	tokenString, err = token.SignedString(secret)
	return 
}

func CheckJWT(tokenstring string, secret string, subject string) (bool, error) {
	subject = strings.ToLower(subject)
	token, err := jwt.Parse(tokenstring, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return false, fmt.Errorf("Unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return false, fmt.Errorf("Invalid token [sub:%s]", subject)
	}
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		if !ok {
			return false, fmt.Errorf("Cannot get claims from token [sub:%s]", subject)
		}
		if strings.ToLower(claims["sub"].(string)) == subject {
			return true, nil
		} else {
			return false, fmt.Errorf("Invalid subject [%s]. Should be [%s]", claims["sub"].(string), subject)
		}
	} else {
		return false, fmt.Errorf("Token not valid[sub:%s]", subject)
	}
}

func singleJoiningSlash(a, b string) string {
	return strings.TrimRight(a, "/") + "/" + strings.TrimLeft(b, "/")
}
