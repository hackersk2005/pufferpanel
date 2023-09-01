/*
 Copyright 2020 Padduck, LLC
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

package oauth2

import (
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/pufferpanel/pufferpanel/v3"
	"github.com/pufferpanel/pufferpanel/v3/logging"
	"github.com/pufferpanel/pufferpanel/v3/middleware"
	"github.com/pufferpanel/pufferpanel/v3/oauth2"
	"github.com/pufferpanel/pufferpanel/v3/response"
	"github.com/pufferpanel/pufferpanel/v3/services"
	"net/http"
	"strings"
	"time"
)

const expiresIn = int64(time.Hour / time.Second)

func registerTokens(g *gin.RouterGroup) {
	g.POST("/token", middleware.NeedsDatabase, handleTokenRequest)
	g.OPTIONS("/token", response.CreateOptions("POST"))
}

// @Summary Authenticate
// @Description Get a OAuth2 token to consume this API
// @Param request body oauth2TokenRequest true "OAuth2 token request"
// @Success 200 {object} oauth2.TokenResponse
// @Failure 400 {object} oauth2.ErrorResponse
// @Failure 401 {object} oauth2.ErrorResponse
// @Failure 500 {object} oauth2.ErrorResponse
// @Router /oauth2/token [post]
func handleTokenRequest(c *gin.Context) {
	var request oauth2TokenRequest
	err := c.MustBindWith(&request, binding.FormPost)
	if err != nil {
		c.JSON(http.StatusBadRequest, &oauth2.ErrorResponse{Error: "invalid_request", ErrorDescription: err.Error()})
		return
	}

	db := middleware.GetDatabase(c)
	if db == nil {
		c.JSON(http.StatusInternalServerError, &oauth2.ErrorResponse{Error: "invalid_request", ErrorDescription: "database not available"})
		return
	}

	session := &services.Session{DB: db}

	switch strings.ToLower(request.GrantType) {
	case "client_credentials":
		{
			os := &services.OAuth2{DB: db}
			client, err := os.Get(request.ClientId)
			if err != nil {
				c.JSON(http.StatusBadRequest, &oauth2.ErrorResponse{Error: "invalid_request", ErrorDescription: err.Error()})
				return
			}

			if !client.ValidateSecret(request.ClientSecret) {
				c.JSON(http.StatusBadRequest, &oauth2.ErrorResponse{Error: "invalid_client"})
				return
			}

			//if the client we have has 0 scopes, then we will have to pull the rights the user has
			if len(client.Scopes) == 0 {
				ps := &services.Permission{DB: db}
				var serverId *string
				if client.ServerId != "" {
					serverId = &client.ServerId
				}
				perms, err := ps.GetForUserAndServer(client.UserId, serverId)
				if err != nil {
					c.JSON(http.StatusBadRequest, &oauth2.ErrorResponse{Error: "invalid_request", ErrorDescription: err.Error()})
					return
				}
				client.Scopes = perms.Scopes
			}

			token, err := session.CreateForClient(client)
			if err != nil {
				c.JSON(http.StatusBadRequest, &oauth2.ErrorResponse{Error: "invalid_request", ErrorDescription: err.Error()})
				return
			}

			c.JSON(http.StatusOK, &oauth2.TokenResponse{
				AccessToken: token,
				TokenType:   "Bearer",
				Scope:       pufferpanel.ScopeOAuth2Auth.Value,
				ExpiresIn:   expiresIn,
			})
			return
		}
	case "password":
		{
			auth := strings.TrimSpace(c.GetHeader("Authorization"))
			if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
				c.Header("WWW-Authenticate", "Bearer")
				c.JSON(http.StatusUnauthorized, &oauth2.ErrorResponse{Error: "invalid_client"})
				return
			}

			//validate this is a bearer token and a good JWT token
			auth = strings.TrimPrefix(auth, "Bearer ")
			node, err := session.ValidateNode(auth)
			if err != nil {
				c.JSON(http.StatusBadRequest, &oauth2.ErrorResponse{Error: "invalid_request", ErrorDescription: err.Error()})
				return
			}

			us := &services.User{DB: db}
			ss := &services.Server{DB: db}

			//get user and server information
			parts := strings.SplitN(request.Username, "|", 2)
			user, err := us.GetByEmail(parts[0])
			if err != nil {
				c.JSON(http.StatusBadRequest, &oauth2.ErrorResponse{Error: "invalid_request", ErrorDescription: err.Error()})
				return
			}

			server, err := ss.Get(parts[1])
			if err != nil {
				c.JSON(http.StatusBadRequest, &oauth2.ErrorResponse{Error: "invalid_request", ErrorDescription: err.Error()})
				return
			}

			//ensure the node asking for the credential check is where this server is
			if server.NodeID != node.ID {
				c.JSON(http.StatusBadRequest, &oauth2.ErrorResponse{Error: "invalid_request", ErrorDescription: "no access"})
				return
			}

			//confirm user has access to this server
			ps := &services.Permission{DB: db}
			perms, err := ps.GetForUserAndServer(user.ID, &server.Identifier)
			if err != nil {
				c.JSON(http.StatusBadRequest, &oauth2.ErrorResponse{Error: "invalid_request", ErrorDescription: err.Error()})
				return
			}
			if perms.ID == 0 || !pufferpanel.ContainsScope(perms.Scopes, pufferpanel.ScopeServerSftp) {
				c.JSON(http.StatusBadRequest, &oauth2.ErrorResponse{Error: "invalid_request", ErrorDescription: "no access"})
				return
			}

			//validate their credentials
			var token string
			user, _, err = us.ValidateLogin(user.Email, request.Password)
			if err != nil {
				c.JSON(http.StatusBadRequest, &oauth2.ErrorResponse{Error: "invalid_request", ErrorDescription: "no access"})
				return
			}

			//at this point, their login credentials were valid, and we need to shortcut because otp
			sessionService := &services.Session{DB: db}
			token, err = sessionService.CreateForUser(user)
			if err != nil {
				logging.Error.Printf("Error generating token: %s", err.Error())
				c.JSON(http.StatusBadRequest, &oauth2.ErrorResponse{Error: "invalid_request", ErrorDescription: "no access"})
				return
			}

			mappedScopes := make([]string, 0)

			for _, p := range perms.Scopes {
				mappedScopes = append(mappedScopes, server.Identifier+":"+p.Value)
			}

			c.JSON(http.StatusOK, &oauth2.TokenResponse{
				AccessToken: token,
				TokenType:   "Bearer",
				Scope:       strings.Join(mappedScopes, " "),
				ExpiresIn:   expiresIn,
			})
		}
	default:
		c.JSON(http.StatusBadRequest, &oauth2.ErrorResponse{Error: "unsupported_grant_type"})
	}
}

type oauth2TokenRequest struct {
	GrantType    string `form:"grant_type"`
	ClientId     string `form:"client_id"`
	ClientSecret string `form:"client_secret"`
	Username     string `form:"username"`
	Password     string `form:"password"`
} //@name OAuth2TokenRequest
