package webserver

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"
	"strings"
	"time"

	mythicConfig "github.com/MythicMeta/MythicContainer/config"
	"github.com/MythicMeta/MythicContainer/logging"
	"github.com/gin-gonic/gin"
)

var mythicClient = &http.Client{Timeout: 30 * time.Second}

func Initialize(configInstance instanceConfig) *gin.Engine {
	if mythicConfig.MythicConfig.DebugLevel == "warning" {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}
	r := gin.New()
	gin.DisableConsoleColor()
	// Global middleware
	r.Use(InitializeGinLogger(configInstance))
	// Recovery middleware recovers from any panics and writes a 500 if there was one.
	r.Use(gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		if err, ok := recovered.(string); ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("error: %s", err)})
		}
		c.AbortWithStatus(http.StatusInternalServerError)
	}))
	r.RedirectFixedPath = true
	r.HandleMethodNotAllowed = true
	r.RemoveExtraSlash = true
	r.MaxMultipartMemory = 8 << 20 // 8 MB
	// set up the routes to use
	setRoutes(r, configInstance)
	return r
}

func StartServer(r *gin.Engine, configInstance instanceConfig) {
	logging.LogInfo("Starting webserver", "config", configInstance)
	if configInstance.UseSSL {
		if err := checkCerts(configInstance.CertPath, configInstance.KeyPath); err != nil {
			// certs don't exist, so generate them
			if err = generateCerts(configInstance); err != nil {
				logging.LogFatalError(err, "Failed to generate certs")
			}
		}
		if configInstance.BindIP != "" {
			go backgroundRunTLS(r, fmt.Sprintf("%s:%d", configInstance.BindIP, configInstance.Port), configInstance.CertPath, configInstance.KeyPath)
		} else {
			go backgroundRunTLS(r, fmt.Sprintf("%s:%d", "0.0.0.0", configInstance.Port), configInstance.CertPath, configInstance.KeyPath)
		}
	} else {
		if configInstance.BindIP != "" {
			go backgroundRun(r, fmt.Sprintf("%s:%d", configInstance.BindIP, configInstance.Port))
		} else {
			go backgroundRun(r, fmt.Sprintf("%s:%d", "0.0.0.0", configInstance.Port))
		}

	}
}

func backgroundRun(r *gin.Engine, address string) {
	if err := r.Run(address); err != nil {
		logging.LogFatalError(err, "Failed to run webserver")
	}
}
func backgroundRunTLS(r *gin.Engine, address string, certPath string, keyPath string) {
	if err := r.RunTLS(address, certPath, keyPath); err != nil {
		logging.LogFatalError(err, "Failed to run webserver")
	}
}

func InitializeGinLogger(configInstance instanceConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Start timer
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery
		//logging.LogDebug("got new request")
		// Process request
		c.Next()
		param := gin.LogFormatterParams{
			Request: c.Request,
			Keys:    c.Keys,
		}

		// Stop timer
		param.TimeStamp = time.Now()
		param.Latency = param.TimeStamp.Sub(start)

		param.ClientIP = c.ClientIP()
		param.Method = c.Request.Method
		param.StatusCode = c.Writer.Status()
		param.ErrorMessage = c.Errors.ByType(gin.ErrorTypePrivate).String()

		param.BodySize = c.Writer.Size()

		if raw != "" {
			path = path + "?" + raw
		}

		param.Path = path
		if configInstance.Debug {
			logging.LogInfo("WebServer Logging",
				"ClientIP", param.ClientIP,
				"method", param.Method,
				"path", param.Path,
				"protocol", param.Request.Proto,
				"statusCode", param.StatusCode,
				"latency", param.Latency.String(),
				"error", param.ErrorMessage)
		}
		c.Next()
	}
}

func setRoutes(r *gin.Engine, configInstance instanceConfig) {
	// define generic get/post routes

	// Track (method, path) pairs that have been claimed by registered
	// variations so we can (a) tolerate duplicate URIs across variations
	// without Gin panicking and (b) know whether a custom variation has
	// already claimed the "POST /" default fallback slot.
	registered := map[string]bool{}
	claim := func(method, path string) bool {
		key := method + " " + path
		if registered[key] {
			return false
		}
		registered[key] = true
		return true
	}

	for _, variation := range AgentConfigs {
		for _, uri := range variation.Get.URIs {
			method := "POST"
			if variation.Get.Verb == "GET" {
				method = "GET"
			}
			if !claim(method, uri) {
				logging.LogInfo("Skipping duplicate route", "method", method, "uri", uri, "variation", variation.Name)
				continue
			}
			logging.LogInfo("Setting Agent Config GET",
				"verb", variation.Get.Verb,
				"uri", uri,
				"location", variation.Get.Client.Message.Location,
				"name", variation.Get.Client.Message.Name)
			if method == "GET" {
				r.GET(uri, proxyRequest(configInstance, variation.Get))
			} else {
				r.POST(uri, proxyRequest(configInstance, variation.Get))
			}
		}
		for _, uri := range variation.Post.URIs {
			method := "POST"
			if variation.Post.Verb == "GET" {
				method = "GET"
			}
			if !claim(method, uri) {
				logging.LogInfo("Skipping duplicate route", "method", method, "uri", uri, "variation", variation.Name)
				continue
			}
			logging.LogInfo("Setting Agent Config POST",
				"verb", variation.Post.Verb,
				"uri", uri,
				"location", variation.Post.Client.Message.Location,
				"name", variation.Post.Client.Message.Location)
			if method == "GET" {
				r.GET(uri, proxyRequest(configInstance, variation.Post))
			} else {
				r.POST(uri, proxyRequest(configInstance, variation.Post))
			}
		}

	}

	// Always-on fallback for agents built with an empty raw_c2_config
	// (or built before their variation made it into agent_configs.json):
	// accept POST / with the message in the body and no transforms.
	// Only register if a custom variation hasn't already claimed POST /.
	if claim("POST", "/") {
		defaultVariation := AgentVariationConfig{
			Verb: "POST",
			URIs: []string{"/"},
			Client: AgentVariationConfigClient{
				Message: AgentVariationConfigMessage{Location: "body"},
			},
		}
		logging.LogInfo("Registering default fallback route", "method", "POST", "uri", "/", "location", "body")
		r.POST("/", proxyRequest(configInstance, defaultVariation))
	}
	if len(configInstance.PayloadHostPaths) > 0 {
		for path, value := range configInstance.PayloadHostPaths {
			localVal := value
			directorForFiles := func(req *http.Request) {
				req.URL.Scheme = "http"
				req.URL.Host = fmt.Sprintf("%s:%d", mythicConfig.MythicConfig.MythicServerHost, mythicConfig.MythicConfig.MythicServerPort)
				req.Host = fmt.Sprintf("%s:%d", mythicConfig.MythicConfig.MythicServerHost, mythicConfig.MythicConfig.MythicServerPort)
				req.URL.Path = fmt.Sprintf("/direct/download/%s", localVal)
				req.Header.Add("mythic", "httpx")
			}
			proxyForFiles := httputil.ReverseProxy{Director: directorForFiles,
				Transport: &http.Transport{
					DialContext: (&net.Dialer{
						Timeout: 30 * time.Second,
					}).DialContext,
					MaxIdleConns:    10,
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				}}
			r.GET(path, generateServeFile(configInstance, fmt.Sprintf("%s", localVal), &proxyForFiles))
		}
	}
}

func generateServeFile(configInstance instanceConfig, fileUUID string, proxyForFiles *httputil.ReverseProxy) gin.HandlerFunc {
	if configInstance.Debug {
		logging.LogInfo("debug route", "host", mythicConfig.MythicConfig.MythicServerHost, "path", "/direct/download/"+fileUUID)
	}
	return func(c *gin.Context) {
		proxyForFiles.ServeHTTP(c.Writer, c.Request)
	}
}
func transformMessageFromServer(message []byte, variation AgentVariationConfig) ([]byte, error) {
	result := message
	var err error
	for i := 0; i < len(variation.Server.Transforms); i++ {
		//logging.LogInfo("configuring message from server", "transform", variation.Server.Transforms[i].Action)
		switch strings.ToLower(variation.Server.Transforms[i].Action) {
		case "base64":
			result, err = transformBase64(result, variation.Server.Transforms[i].Value)
			if err != nil {
				return nil, err
			}
		case "base64url":
			result, err = transformBase64URL(result, variation.Server.Transforms[i].Value)
			if err != nil {
				return nil, err
			}
		case "prepend":
			result, err = transformPrepend(result, variation.Server.Transforms[i].Value)
			if err != nil {
				return nil, err
			}
		case "append":
			result, err = transformAppend(result, variation.Server.Transforms[i].Value)
			if err != nil {
				return nil, err
			}
		case "xor":
			result, err = transformXor(result, variation.Server.Transforms[i].Value)
			if err != nil {
				return nil, err
			}
		case "netbios":
			result, err = transformNetbios(result, variation.Server.Transforms[i].Value)
			if err != nil {
				return nil, err
			}
		case "netbiosu":
			result, err = transformNetbiosu(result, variation.Server.Transforms[i].Value)
			if err != nil {
				return nil, err
			}
		default:
			return nil, errors.New(fmt.Sprintf("unknown action in transform: %s", strings.ToLower(variation.Server.Transforms[i].Action)))
		}
	}
	return result, nil
}
func transformMessageFromClient(message []byte, variation AgentVariationConfig) ([]byte, error) {
	result := message
	var err error
	for i := len(variation.Client.Transforms) - 1; i >= 0; i-- {
		//logging.LogInfo("getting message from client", "transform", variation.Client.Transforms[i].Action)
		switch strings.ToLower(variation.Client.Transforms[i].Action) {
		case "base64":
			result, err = transformBase64Reverse(result, variation.Client.Transforms[i].Value)
			if err != nil {
				return nil, err
			}
		case "base64url":
			result, err = transformBase64URLReverse(result, variation.Client.Transforms[i].Value)
			if err != nil {
				return nil, err
			}
		case "prepend":
			result, err = transformPrependReverse(result, variation.Client.Transforms[i].Value)
			if err != nil {
				return nil, err
			}
		case "append":
			result, err = transformAppendReverse(result, variation.Client.Transforms[i].Value)
			if err != nil {
				return nil, err
			}
		case "xor":
			result, err = transformXorReverse(result, variation.Client.Transforms[i].Value)
			if err != nil {
				return nil, err
			}
		case "netbios":
			result, err = transformNetbiosReverse(result, variation.Client.Transforms[i].Value)
			if err != nil {
				return nil, err
			}
		case "netbiosu":
			result, err = transformNetbiosuReverse(result, variation.Client.Transforms[i].Value)
			if err != nil {
				return nil, err
			}
		default:
			return nil, errors.New(fmt.Sprintf("unknown action in transform: %s", strings.ToLower(variation.Client.Transforms[i].Action)))
		}
	}
	return result, nil
}
func getMessageFromClient(req *http.Request, variation AgentVariationConfig) ([]byte, error) {
	logging.LogInfo("Getting message from client", "location", variation.Client.Message.Location)
	switch strings.ToLower(variation.Client.Message.Location) {
	case "cookie":
		cookie, err := req.Cookie(variation.Client.Message.Name)
		if err != nil {
			logging.LogError(err, "Failed to get cookie")
			return nil, err
		}
		return transformMessageFromClient([]byte(cookie.Value), variation)
	case "query":
		params := req.URL.Query()
		if params.Has(variation.Client.Message.Name) {
			param := params.Get(variation.Client.Message.Name)
			return transformMessageFromClient([]byte(param), variation)
		}
		return nil, errors.New("failed to find form variable")
	case "header":
		return transformMessageFromClient([]byte(req.Header.Get(variation.Client.Message.Name)), variation)
	default:
		if req.ContentLength > 0 {
			body, err := io.ReadAll(req.Body)
			req.Body.Close()
			if err != nil {
				logging.LogError(err, "Failed to read body")
				return nil, err
			}
			return transformMessageFromClient(body, variation)
		}
		return nil, errors.New("body is empty but message indicated in body")
	}
}
func proxyRequest(configInstance instanceConfig, variation AgentVariationConfig) gin.HandlerFunc {
	if configInstance.Debug {
		logging.LogInfo("debug route", "host", mythicConfig.MythicConfig.MythicServerHost, "path", "/agent_message")
	}
	return func(c *gin.Context) {
		agentMessage, err := getMessageFromClient(c.Request, variation)
		if err != nil {
			logging.LogError(err, "Failed to get message from client to proxy to mythic")
			c.AbortWithStatus(http.StatusBadGateway)
			return
		}
		upstreamURL := fmt.Sprintf("http://%s:%d/agent_message", mythicConfig.MythicConfig.MythicServerHost, mythicConfig.MythicConfig.MythicServerPort)
		upstreamReq, err := http.NewRequest(http.MethodPost, upstreamURL, bytes.NewReader(agentMessage))
		if err != nil {
			logging.LogError(err, "Failed to create upstream mythic request")
			c.AbortWithStatus(http.StatusBadGateway)
			return
		}
		upstreamReq.Header.Set("mythic", "httpx")
		upstreamReq.Header.Set("Content-Length", strconv.Itoa(len(agentMessage)))
		upstreamReq.ContentLength = int64(len(agentMessage))
		upstreamReq.TransferEncoding = nil

		resp, err := mythicClient.Do(upstreamReq)
		if err != nil {
			logging.LogError(err, "Failed to send decoded agent message to mythic")
			c.AbortWithStatus(http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		originalMessage, err := io.ReadAll(resp.Body)
		if err != nil {
			logging.LogError(err, "failed to get message body from mythic's response")
			c.AbortWithStatus(http.StatusBadGateway)
			return
		}
		agentResponse, err := transformMessageFromServer(originalMessage, variation)
		if err != nil {
			logging.LogError(err, "failed to create transformed response for agent")
			c.AbortWithStatus(http.StatusBadGateway)
			return
		}
		for key, val := range variation.Server.Headers {
			c.Writer.Header().Set(key, val)
		}
		c.Writer.Header().Set("Content-Length", strconv.Itoa(len(agentResponse)))
		c.Data(resp.StatusCode, c.Writer.Header().Get("Content-Type"), agentResponse)
	}
}

// code to generate self-signed certs pulled from github.com/kabukky/httpscerts
// and from http://golang.org/src/crypto/tls/generate_cert.go.
// only modifications were to use a specific elliptic curve cipher
func checkCerts(certPath string, keyPath string) error {
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		return err
	} else if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		return err
	}
	return nil
}
func generateCerts(configInstance instanceConfig) error {

	logging.LogInfo("[*] generating certs now...")
	priv, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		logging.LogError(err, "failed to generate private key")
		return err
	}
	notBefore := time.Now()
	oneYear := 365 * 24 * time.Hour
	notAfter := notBefore.Add(oneYear)
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		logging.LogError(err, "failed to generate serial number")
		return err
	}
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Mythic C2"},
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		logging.LogError(err, "failed to create certificate")
		return err
	}
	certOut, err := os.Create(configInstance.CertPath)
	if err != nil {
		logging.LogError(err, "failed to open "+configInstance.CertPath+" for writing")
		return err
	}
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	certOut.Close()
	keyOut, err := os.OpenFile(configInstance.KeyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		logging.LogError(err, "failed to open "+configInstance.KeyPath+" for writing")
		return err
	}
	marshalKey, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		logging.LogError(err, "Unable to marshal ECDSA private key")
		return err
	}
	pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: marshalKey})
	keyOut.Close()
	logging.LogInfo("Successfully generated new SSL certs\n")
	return nil
}
