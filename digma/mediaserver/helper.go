package mediaserver

import (
	"fmt"
	"strings"

	"github.com/dgrijalva/jwt-go"
)

func checkJWT(tokenstring string, secret string, subject string) (bool, error) {
	type ClaimSet struct {
		Subject string `json:"sub,omitempty"`
		Expires int64  `json:"exp,string"`
	}
	token, _ := jwt.Parse(tokenstring, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return false, fmt.Errorf("Unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		if strings.ToLower(claims["sub"].(string)) == strings.ToLower(subject) {
			return true, nil
		} else {
			return false, fmt.Errorf("Invalid subject [%s]. Should be [%s]", claims["sub"].(string), subject)
		}
	} else {
		return false, fmt.Errorf("Token not valid")
	}
}
