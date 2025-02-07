package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/config/client/api"
	"github.com/pion/logging"
)

type LicenseStatusClient interface {
	LicenseStatus(ctx context.Context) (stnrv1.LicenseStatus, error)
}

type licenseStatusClient struct {
	addr, httpURI string
	client        *api.ClientWithResponses
	logging.LeveledLogger
}

func NewLicenseStatusClient(addr string, logger logging.LeveledLogger, opts ...ClientOption) (LicenseStatusClient, error) {
	httpuri, err := getURI(addr)
	if err != nil {
		return nil, err
	}

	client, err := api.NewClientWithResponses(httpuri.String(), opts...)
	if err != nil {
		return nil, err
	}

	return &licenseStatusClient{
		addr:          addr,
		httpURI:       httpuri.String(),
		client:        client,
		LeveledLogger: logger,
	}, nil
}

func (a *licenseStatusClient) LicenseStatus(ctx context.Context) (stnrv1.LicenseStatus, error) {
	a.Debugf("GET: loading license status from CDS server %s", a.addr)

	s := stnrv1.NewEmptyLicenseStatus()
	r, err := a.client.GetV1LicenseStatusWithResponse(ctx)
	if err != nil {
		return s, err
	}

	if r.HTTPResponse.StatusCode != http.StatusOK {
		body := strings.TrimSpace(string(r.Body))
		return s, fmt.Errorf("HTTP error (status: %s): %s", r.HTTPResponse.Status, body)
	}

	if err := json.Unmarshal(r.Body, &s); err != nil {
		return stnrv1.NewEmptyLicenseStatus(), err
	}

	return s, nil
}
