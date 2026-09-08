package torrent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Aria2RPCClient struct {
	Client *http.Client
	URL    string
	Secret string
}

type RPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      string        `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type RPCResponse struct {
	Error   *RPCError       `json:"error,omitempty"`
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
}

type RPCError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

type Aria2Status struct {
	GID             string   `json:"gid"`
	Status          string   `json:"status"`
	TotalLength     string   `json:"totalLength"`
	CompletedLength string   `json:"completedLength"`
	DownloadSpeed   string   `json:"downloadSpeed"`
	Connections     string   `json:"connections"`
	ErrorMessage    string   `json:"errorMessage,omitempty"`
	FollowedBy      []string `json:"followedBy,omitempty"`
	Following       string   `json:"following,omitempty"`
	Files           []struct {
		Path string `json:"path"`
	} `json:"files"`
}

func NewAria2RPCClient(url, secret string) *Aria2RPCClient {
	return &Aria2RPCClient{
		URL:    url,
		Secret: secret,
		Client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Aria2RPCClient) call(method string, params ...interface{}) (json.RawMessage, error) {
	rpcParams := []interface{}{}
	if c.Secret != "" {
		rpcParams = append(rpcParams, "token:"+c.Secret)
	}
	rpcParams = append(rpcParams, params...)

	reqBody := RPCRequest{
		JSONRPC: "2.0",
		ID:      "zee-mirror",
		Method:  method,
		Params:  rpcParams,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	resp, err := c.Client.Post(c.URL, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rpcResp RPCResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, err
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("aria2 rpc error: %s (code: %d)", rpcResp.Error.Message, rpcResp.Error.Code)
	}

	return rpcResp.Result, nil
}

func (c *Aria2RPCClient) AddURI(uri string, options map[string]interface{}) (string, error) {
	res, err := c.call("aria2.addUri", []string{uri}, options)
	if err != nil {
		return "", err
	}
	var gid string
	err = json.Unmarshal(res, &gid)
	return gid, err
}

func (c *Aria2RPCClient) AddTorrent(torrentBase64 string, options map[string]interface{}) (string, error) {
	res, err := c.call("aria2.addTorrent", torrentBase64, []string{}, options)
	if err != nil {
		return "", err
	}
	var gid string
	err = json.Unmarshal(res, &gid)
	return gid, err
}

func (c *Aria2RPCClient) TellStatus(gid string) (*Aria2Status, error) {
	res, err := c.call("aria2.tellStatus", gid)
	if err != nil {
		return nil, err
	}
	var status Aria2Status
	err = json.Unmarshal(res, &status)
	return &status, err
}

func (c *Aria2RPCClient) Remove(gid string) error {
	_, err := c.call("aria2.forceRemove", gid)
	return err
}

func (c *Aria2RPCClient) GetVersion() (string, error) {
	res, err := c.call("aria2.getVersion")
	if err != nil {
		return "", err
	}
	var version struct {
		Version string `json:"version"`
	}
	err = json.Unmarshal(res, &version)
	return version.Version, err
}
