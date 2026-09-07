package files

import (
	"net/url"
	"testing"
	"time"

	"github.com/cozy/cozy-stack/pkg/config/config"
	"github.com/cozy/cozy-stack/pkg/consts"
	"github.com/cozy/cozy-stack/tests/testutils"
	"github.com/cozy/cozy-stack/web/errors"
	"github.com/cozy/cozy-stack/web/middlewares"
	"github.com/gavv/httpexpect/v2"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

func TestPermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("an instance is required for this test: test skipped due to the use of --short flag")
	}

	config.UseTestFile(t)
	require.NoError(t, loadLocale(), "Could not load default locale translations")

	testutils.NeedCouchdb(t)
	setup := testutils.NewSetup(t, t.Name())

	config.GetConfig().Fs.URL = &url.URL{
		Scheme: "file",
		Host:   "localhost",
		Path:   t.TempDir(),
	}

	testInstance := setup.GetTestInstance()
	client, token := setup.GetTestClient(consts.Files + " " + consts.CertifiedCarbonCopy + " " + consts.CertifiedElectronicSafe)

	ts := setup.GetTestServer("/files", Routes, func(r *echo.Echo) *echo.Echo {
		secure := middlewares.Secure(&middlewares.SecureConfig{
			CSPDefaultSrc:     []middlewares.CSPSource{middlewares.CSPSrcSelf},
			CSPFrameAncestors: []middlewares.CSPSource{middlewares.CSPSrcNone},
		})
		r.Use(secure)
		return r
	})
	ts.Config.Handler.(*echo.Echo).HTTPErrorHandler = errors.ErrorHandler
	t.Cleanup(ts.Close)

	t.Run("CreateDirNoToken", func(t *testing.T) {
		e := testutils.CreateTestClient(t, ts.URL)

		// Missing the "Authorization" header
		e.POST("/files/").
			WithQuery("Name", "foo").
			WithQuery("Type", "directory").
			Expect().Status(401)
	})

	t.Run("CreateDirBadType", func(t *testing.T) {
		e := testutils.CreateTestClient(t, ts.URL)

		badtok, _ := testInstance.MakeJWT(consts.AccessTokenAudience, client.ClientID, "io.cozy.events", "", time.Now())

		e.POST("/files/").
			WithQuery("Name", "foo").
			WithQuery("Type", "directory").
			WithHeader("Authorization", "Bearer "+badtok).
			Expect().Status(403)
	})

	t.Run("CreateDirWildCard", func(t *testing.T) {
		e := testutils.CreateTestClient(t, ts.URL)

		wildTok, _ := testInstance.MakeJWT(consts.AccessTokenAudience, client.ClientID, "io.cozy.files.*", "", time.Now())

		e.POST("/files/").
			WithQuery("Name", "icancreateyou").
			WithQuery("Type", "directory").
			WithHeader("Authorization", "Bearer "+wildTok).
			Expect().Status(201)
	})

	t.Run("CreateDirLimitedScope", func(t *testing.T) {
		e := testutils.CreateTestClient(t, ts.URL)

		// Create dir "/permissionholder"
		dirID := e.POST("/files/").
			WithQuery("Name", "permissionholder").
			WithQuery("Type", "directory").
			WithHeader("Authorization", "Bearer "+token).
			Expect().Status(201).
			JSON(httpexpect.ContentOpts{MediaType: "application/vnd.api+json"}).
			Object().Path("$.data.id").String().NotEmpty().Raw()

		badtok, _ := testInstance.MakeJWT(consts.AccessTokenAudience, client.ClientID, "io.cozy.files:ALL:"+dirID, "", time.Now())

		// not in authorized dir
		e.POST("/files/").
			WithQuery("Name", "icantcreateyou").
			WithQuery("Type", "directory").
			WithHeader("Authorization", "Bearer "+badtok).
			Expect().Status(403)

		// in authorized dir
		e.POST("/files/").
			WithQuery("Name", "icantcreateyou").
			WithQuery("Type", "directory").
			WithHeader("Authorization", "Bearer "+token).
			Expect().Status(201)
	})

	t.Run("CreateDirBadVerb", func(t *testing.T) {
		e := testutils.CreateTestClient(t, ts.URL)

		badtok, _ := testInstance.MakeJWT(consts.AccessTokenAudience, client.ClientID, "io.cozy.files:GET", "", time.Now())

		e.POST("/files/").
			WithQuery("Name", "icantcreateyou").
			WithQuery("Type", "directory").
			WithHeader("Authorization", "Bearer "+badtok).
			Expect().Status(403)
	})

	t.Run("MoveToForbiddenDestinationDenied", func(t *testing.T) {
		e := testutils.CreateTestClient(t, ts.URL)

		createDir := func(name string) string {
			return e.POST("/files/").
				WithQuery("Name", name).
				WithQuery("Type", "directory").
				WithHeader("Authorization", "Bearer "+token).
				Expect().Status(201).
				JSON(httpexpect.ContentOpts{MediaType: "application/vnd.api+json"}).
				Object().Path("$.data.id").String().NotEmpty().Raw()
		}
		srcDirID := createDir("move-src")
		dstDirID := createDir("move-dst")

		fileID := e.POST("/files/"+srcDirID).
			WithQuery("Name", "movable.txt").
			WithQuery("Type", "file").
			WithHeader("Content-Type", "text/plain").
			WithHeader("Authorization", "Bearer "+token).
			WithBytes([]byte("hello")).
			Expect().Status(201).
			JSON(httpexpect.ContentOpts{MediaType: "application/vnd.api+json"}).
			Object().Path("$.data.id").String().NotEmpty().Raw()

		// The token can PATCH the file through its parent directory, but
		// cannot POST into the destination directory.
		srctok, _ := testInstance.MakeJWT(consts.AccessTokenAudience, client.ClientID, "io.cozy.files:ALL:"+srcDirID, "", time.Now())

		e.PATCH("/files/"+fileID).
			WithHeader("Content-Type", "application/json").
			WithHeader("Authorization", "Bearer "+srctok).
			WithBytes([]byte(`{"data":{"type":"io.cozy.files","id":"` + fileID + `","attributes":{"dir_id":"` + dstDirID + `"}}}`)).
			Expect().Status(403)
	})

	t.Run("MoveToMissingDestinationNotFound", func(t *testing.T) {
		e := testutils.CreateTestClient(t, ts.URL)

		fileID := e.POST("/files/").
			WithQuery("Name", "orphan-move.txt").
			WithQuery("Type", "file").
			WithHeader("Content-Type", "text/plain").
			WithHeader("Authorization", "Bearer "+token).
			WithBytes([]byte("hello")).
			Expect().Status(201).
			JSON(httpexpect.ContentOpts{MediaType: "application/vnd.api+json"}).
			Object().Path("$.data.id").String().NotEmpty().Raw()

		e.PATCH("/files/"+fileID).
			WithHeader("Content-Type", "application/json").
			WithHeader("Authorization", "Bearer "+token).
			WithBytes([]byte(`{"data":{"type":"io.cozy.files","id":"` + fileID + `","attributes":{"dir_id":"does-not-exist"}}}`)).
			Expect().Status(404)
	})
}
