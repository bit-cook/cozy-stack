package client

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cozy/cozy-stack/client/request"
	"github.com/cozy/cozy-stack/pkg/config/config"
	"github.com/cozy/cozy-stack/pkg/consts"
	"github.com/cozy/cozy-stack/tests/testutils"
	weberrors "github.com/cozy/cozy-stack/web/errors"
	webfiles "github.com/cozy/cozy-stack/web/files"
	"github.com/cozy/cozy-stack/web/middlewares"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// filesTestClient creates a minimal live files server and an authenticated
// client ready to call files routes.
func filesTestClient(t *testing.T) *Client {
	config.UseTestFile(t)
	testutils.NeedCouchdb(t)
	setup := testutils.NewSetup(t, t.Name())
	config.GetConfig().Fs.URL = &url.URL{Scheme: "file", Host: "localhost", Path: t.TempDir()}

	inst := setup.GetTestInstance()
	_, token := setup.GetTestClient(consts.Files + " " + consts.CertifiedCarbonCopy + " " + consts.CertifiedElectronicSafe)
	ts := setup.GetTestServer("/files", webfiles.Routes, func(r *echo.Echo) *echo.Echo {
		secure := middlewares.Secure(&middlewares.SecureConfig{CSPDefaultSrc: []middlewares.CSPSource{middlewares.CSPSrcSelf}, CSPFrameAncestors: []middlewares.CSPSource{middlewares.CSPSrcNone}})
		r.Use(secure)
		return r
	})
	ts.Config.Handler.(*echo.Echo).HTTPErrorHandler = weberrors.ErrorHandler
	t.Cleanup(ts.Close)

	u, err := url.Parse(ts.URL)
	require.NoError(t, err)
	hostPort := u.Host
	if _, _, e := net.SplitHostPort(hostPort); e != nil {
		hostPort = u.Host
	}

	return &Client{
		Addr:       hostPort,
		Domain:     inst.Domain,
		Scheme:     u.Scheme,
		Client:     &http.Client{Timeout: 10 * time.Second},
		Authorizer: &request.BearerAuthorizer{Token: token},
	}
}

// withListChildrenPageSize temporarily overrides ListChildrenPageSize.
func withListChildrenPageSize(t *testing.T, size string) {
	old := ListChildrenPageSize
	ListChildrenPageSize = size
	t.Cleanup(func() { ListChildrenPageSize = old })
}

func TestListChildrenByDirID_Pagination(t *testing.T) {
	if testing.Short() {
		t.Skip("requires instance; skipped with --short")
	}

	c := NewFilesClient(filesTestClient(t), "")

	parent, err := c.Mkdir("/client-pagination-root")
	require.NoError(t, err)

	withListChildrenPageSize(t, "2")

	names := []string{"a.txt", "b.txt", "c.txt"}
	for _, name := range names {
		_, err := c.Upload(&Upload{
			Name:        name,
			DirID:       parent.ID,
			Contents:    strings.NewReader("foo"),
			ContentType: "text/plain",
		})
		require.NoError(t, err)
	}

	_, err = c.Req(&request.Options{
		Method:  "POST",
		Path:    "/files/" + url.PathEscape(parent.ID),
		Queries: url.Values{"Type": {"directory"}, "Name": {"child"}},
	})
	require.NoError(t, err)

	children, err := c.ListChildrenByDirID(parent.ID)
	require.NoError(t, err)
	assert.Equal(t, 4, len(children))
	gotNames := make(map[string]bool)
	for _, ch := range children {
		gotNames[ch.Attrs.Name] = true
	}
	assert.True(t, gotNames["a.txt"])
	assert.True(t, gotNames["b.txt"])
	assert.True(t, gotNames["c.txt"])
	assert.True(t, gotNames["child"])
}
