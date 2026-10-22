package routes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/bundles/connect/db4fileconnect"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/bundles/preferences"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/http/controllers/api"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/http/middleware"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/filemodel/filedata"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/badges"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/models/forum/fileUsage"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/service/decorationassetservice"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/service/permission"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/resource"
)

// animatedFrameGIF 生成方形、中心透明的多帧 GIF（边框不透明、中心留空）。
func animatedFrameGIF(t *testing.T, frames int) []byte {
	t.Helper()
	palette := color.Palette{color.NRGBA{}, color.NRGBA{R: 200, G: 120, B: 60, A: 255}}
	images := make([]*image.Paletted, 0, frames)
	for i := 0; i < frames; i++ {
		frame := image.NewPaletted(image.Rect(0, 0, 24, 24), palette)
		for y := 0; y < 24; y++ {
			for x := 0; x < 24; x++ {
				if y < 3 || y > 20 || x < 3 || x > 20 {
					frame.SetColorIndex(x, y, 1)
				}
			}
		}
		images = append(images, frame)
	}
	var out bytes.Buffer
	if err := gif.EncodeAll(&out, &gif.GIF{Image: images, Delay: make([]int, frames), Config: image.Config{ColorModel: palette, Width: 24, Height: 24}}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// largeAnimatedFrameGIF 生成真实的大体积 GIF：512×512、中心透明、宽环带用 255 色确定性伪随机注水，
// 因此几乎不可压缩、容易超过 4MB；帧数按目标字节数递增，保持在 48M 合成像素预算内。
func largeAnimatedFrameGIF(t *testing.T, wantBytes int) []byte {
	t.Helper()
	palette := make(color.Palette, 256)
	palette[0] = color.NRGBA{}
	seed := uint32(987654321)
	for i := 1; i < 256; i++ {
		seed = seed*1664525 + 1013904223
		palette[i] = color.NRGBA{R: uint8(seed >> 24), G: uint8(seed >> 16), B: uint8(seed >> 8), A: 255}
	}
	const edge = 512
	build := func(frames int) []byte {
		images := make([]*image.Paletted, 0, frames)
		n := seed
		for i := 0; i < frames; i++ {
			frame := image.NewPaletted(image.Rect(0, 0, edge, edge), palette)
			for y := 0; y < edge; y++ {
				for x := 0; x < edge; x++ {
					dx, dy := x-edge/2, y-edge/2
					d2 := dx*dx + dy*dy
					if d2 < 130*130 || d2 > 250*250 {
						continue
					}
					n = n*1664525 + 1013904223
					frame.SetColorIndex(x, y, uint8(n>>24))
				}
			}
			images = append(images, frame)
		}
		var out bytes.Buffer
		if err := gif.EncodeAll(&out, &gif.GIF{Image: images, Delay: make([]int, frames), LoopCount: 0, Config: image.Config{ColorModel: palette, Width: edge, Height: edge}}); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	frames := 16
	data := build(frames)
	for len(data) < wantBytes && frames < 80 {
		frames += 8
		data = build(frames)
	}
	return data
}

func decorationAssetRequest(t *testing.T, router http.Handler, token, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/decoration-asset", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestDecorationAssetUploadStaticAndLifecycle(t *testing.T) {
	conn, router := setupDecorationHTTP(t)
	if err := db4fileconnect.Connect().AutoMigrate(&filedata.Entity{}); err != nil {
		t.Fatal(err)
	}
	previousEnv := preferences.Get("app.env", "production")
	preferences.Set("app.env", "production")
	t.Cleanup(func() { preferences.Set("app.env", previousEnv) })
	static, err := resource.GetStaticFS()
	if err != nil {
		t.Fatal(err)
	}
	mount := router.Group("/")
	mount.Use(middleware.BrowserCache)
	mount.StaticFS("static", http.FS(decorationStaticFS{embedded: static}))
	admin := router.Group("/api/admin", middleware.CSRFProtection, middleware.JWTAuthCheck, middleware.CheckWritableAccount)
	admin.POST("decoration-asset", middleware.CheckPermission(permission.SiteManager), api.UploadDecorationAsset)
	router.GET("/file/img/*filename", api.GetFileByFileName)
	manager := createContractSiteManager(t, conn)
	ordinary := createHTTPContractUser(t, conn, contractTestID())
	img := image.NewNRGBA(image.Rect(0, 0, 20, 20))
	var valid bytes.Buffer
	if err := png.Encode(&valid, img); err != nil {
		t.Fatal(err)
	}
	if got := decorationAssetRequest(t, router, contractSessionToken(t, ordinary), "frame.png", valid.Bytes()); got.Code != http.StatusForbidden {
		t.Fatalf("ordinary upload: %d %s", got.Code, got.Body.String())
	}
	for _, tc := range []struct {
		filename string
		data     []byte
	}{{"frame.svg", valid.Bytes()}, {"frame.png", []byte("invalid")}} {
		got := decorationAssetRequest(t, router, contractSessionToken(t, manager), tc.filename, tc.data)
		if got.Code != http.StatusBadRequest {
			t.Fatalf("invalid %s: %d %s", tc.filename, got.Code, got.Body.String())
		}
	}
	var count int64
	conn.Model(&fileUsage.Entity{}).Where("target_type = ?", fileUsage.TargetDecorationAsset).Count(&count)
	if count != 0 {
		t.Fatalf("invalid uploads left %d references", count)
	}
	first := decorationAssetRequest(t, router, contractSessionToken(t, manager), "frame.png", valid.Bytes())
	if first.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", first.Code, first.Body.String())
	}
	var uploaded struct {
		Result struct {
			URL string `json:"url"`
		} `json:"result"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	url := uploaded.Result.URL
	if !strings.HasPrefix(url, "/static/decorations/") {
		t.Fatalf("wrong URL %q", url)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(method, url, nil))
		if rec.Code != 200 || rec.Header().Get("Cache-Control") != "public, max-age=18144000" || rec.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("%s asset: %d %v", method, rec.Code, rec.Header())
		}
		if method == http.MethodGet && !bytes.Equal(rec.Body.Bytes(), valid.Bytes()) {
			t.Fatal("wrong asset bytes")
		}
		if method == http.MethodHead && rec.Body.Len() != 0 {
			t.Fatal("HEAD wrote body")
		}
	}
	// 管理员图稿单独放宽到 8MB：4MB 出头但仍在 8MB 内的 GIF 必须能上传（帖子图片仍是 4MB）。
	padded := largeAnimatedFrameGIF(t, 5*1024*1024)
	if len(padded) <= 4*1024*1024 || len(padded) > 8*1024*1024 {
		t.Fatalf("fixture size out of range: %d", len(padded))
	}
	large := decorationAssetRequest(t, router, contractSessionToken(t, manager), "large.gif", padded)
	if large.Code != http.StatusOK {
		t.Fatalf("large gif upload (%d bytes): %d %s", len(padded), large.Code, large.Body.String())
	}
	if err := json.Unmarshal(large.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = filedata.DeleteByName(strings.TrimPrefix(uploaded.Result.URL, "/static/")) })

	// 超过 8MB 时必须是「文件过大」，不能因为 MaxBytesReader 截断而报成「缺少上传文件」。
	oversize := append(animatedFrameGIF(t, 3), make([]byte, 9*1024*1024)...)
	tooLarge := decorationAssetRequest(t, router, contractSessionToken(t, manager), "oversize.gif", oversize)
	if tooLarge.Code != http.StatusBadRequest || !strings.Contains(tooLarge.Body.String(), "upload.file.tooLarge") {
		t.Fatalf("oversize gif: %d %s", tooLarge.Code, tooLarge.Body.String())
	}

	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/static/decorations/missing.png", nil))
	if missing.Code != 404 || missing.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("missing: %d %v", missing.Code, missing.Header())
	}
	// Saving binds the preview to a definition; replacing it releases the old URL.
	save := func(assetURL string) {
		t.Helper()
		body := fmt.Sprintf(`{"code":"frame-test","name":"Frame test","kind":"avatarFrame","lightColors":["#166534"],"darkColors":["#86EFAC"],"assetUrl":%q,"assetScale":1.25,"isEnabled":true,"sortOrder":1}`, assetURL)
		rec := serveAuthSecurityJSON(router, http.MethodPost, "/api/admin/decoration-save", body, contractSessionToken(t, manager))
		if rec.Code != 200 || decodeContractEnvelope(t, rec).Code != 0 {
			t.Fatalf("save definition: %d %s", rec.Code, rec.Body.String())
		}
	}
	save(url)
	name := strings.TrimPrefix(url, "/static/")
	var usage fileUsage.Entity
	if err := conn.Where("file_name = ?", name).First(&usage).Error; err != nil || usage.ExpiresAt != nil {
		t.Fatalf("saved usage: %+v %v", usage, err)
	}
	second := decorationAssetRequest(t, router, contractSessionToken(t, manager), "next.png", valid.Bytes())
	if second.Code != 200 {
		t.Fatalf("second upload: %d %s", second.Code, second.Body.String())
	}
	if err := json.Unmarshal(second.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	newURL := uploaded.Result.URL
	if newURL == url {
		t.Fatal("reused asset URL")
	}
	t.Cleanup(func() { _ = filedata.DeleteByName(strings.TrimPrefix(newURL, "/static/")) })
	save(newURL)
	if err := conn.Where("file_name = ?", name).First(&usage).Error; err != nil || usage.ExpiresAt == nil {
		t.Fatalf("old usage not released: %+v %v", usage, err)
	}
	removed, err := decorationassetservice.CleanupAssets(t.Context(), time.Now(), 10)
	if err != nil || removed != 1 {
		t.Fatalf("cleanup: %d %v", removed, err)
	}
	if file := filedata.GetByName(name); file.Id != 0 {
		t.Fatal("replaced bytes retained")
	}
	conn.Model(&fileUsage.Entity{}).Where("file_name = ?", name).Count(&count)
	if count != 0 {
		t.Fatal("replaced reference retained")
	}
	if file := filedata.GetByName(strings.TrimPrefix(newURL, "/static/")); file.Id == 0 {
		t.Fatal("current artwork was reclaimed")
	}
	old := httptest.NewRecorder()
	router.ServeHTTP(old, httptest.NewRequest(http.MethodGet, url, nil))
	if old.Code != 404 || old.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("old asset: %d %v", old.Code, old.Header())
	}
	// A preview never saved into a definition expires and is physically removed.
	third := decorationAssetRequest(t, router, contractSessionToken(t, manager), "abandoned.png", valid.Bytes())
	if third.Code != 200 {
		t.Fatalf("third upload: %d %s", third.Code, third.Body.String())
	}
	if err := json.Unmarshal(third.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	abandoned := strings.TrimPrefix(uploaded.Result.URL, "/static/")
	if err := conn.Model(&fileUsage.Entity{}).Where("file_name = ?", abandoned).Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	private := httptest.NewRecorder()
	router.ServeHTTP(private, httptest.NewRequest(http.MethodGet, "/file/img/"+abandoned, nil))
	if private.Code != 404 {
		t.Fatalf("expired preview remained public: %d", private.Code)
	}
	removed, err = decorationassetservice.CleanupAssets(t.Context(), time.Now(), 10)
	if err != nil || removed != 1 || filedata.GetByName(abandoned).Id != 0 {
		t.Fatalf("abandoned cleanup: %d %v", removed, err)
	}
	conn.Where("target_type = ?", fileUsage.TargetDecorationAsset).Delete(&fileUsage.Entity{})
}

func TestBadgeAssetFormatsReferencesAndLegacyLinks(t *testing.T) {
	conn, router := setupDecorationHTTP(t)
	if err := conn.AutoMigrate(&badges.Entity{}); err != nil {
		t.Fatal(err)
	}
	if err := db4fileconnect.Connect().AutoMigrate(&filedata.Entity{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Where("code LIKE ?", "asset-badge-%").Delete(&badges.Entity{}) })
	static, err := resource.GetStaticFS()
	if err != nil {
		t.Fatal(err)
	}
	router.StaticFS("static", http.FS(decorationStaticFS{embedded: static}))
	admin := router.Group("/api/admin", middleware.CSRFProtection, middleware.JWTAuthCheck, middleware.CheckWritableAccount, middleware.CheckPermission(permission.SiteManager))
	admin.POST("badge-asset", api.UploadBadgeAsset)
	admin.POST("badge-save", UpButterReq(api.SaveBadge))
	admin.POST("badge-delete", UpButterReq(api.DeleteBadge))
	manager := createContractSiteManager(t, conn)
	token := contractSessionToken(t, manager)
	// Reuse the multipart helper with a small routing adapter; permissions and parsing stay real.
	upload := func(filename string, data []byte, session string) *httptest.ResponseRecorder {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.URL.Path = "/api/admin/badge-asset"
			router.ServeHTTP(w, r)
		})
		return decorationAssetRequest(t, handler, session, filename, data)
	}
	ordinary := createHTTPContractUser(t, conn, contractTestID())
	if rec := upload("badge.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="20" height="15"><rect width="20" height="15"/></svg>`), contractSessionToken(t, ordinary)); rec.Code != http.StatusForbidden {
		t.Fatal(rec.Code)
	}
	images := []struct {
		name        string
		data        []byte
		contentType string
	}{
		{"badge.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="20" height="15"><rect width="20" height="15" fill="red"/></svg>`), "image/png"},
	}
	webp, err := fs.ReadFile(static, "pic/default-avatar.webp")
	if err != nil {
		t.Fatal(err)
	}
	var pngData bytes.Buffer
	badgePNG := image.NewNRGBA(image.Rect(0, 0, 20, 15))
	badgePNG.SetNRGBA(7, 5, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(&pngData, badgePNG); err != nil {
		t.Fatal(err)
	}
	images = append(images, struct {
		name        string
		data        []byte
		contentType string
	}{"badge.webp", webp, "image/png"}, struct {
		name        string
		data        []byte
		contentType string
	}{"badge.png", pngData.Bytes(), "image/png"})
	var gifData bytes.Buffer
	if err := gif.EncodeAll(&gifData, &gif.GIF{Image: []*image.Paletted{image.NewPaletted(image.Rect(0, 0, 20, 15), color.Palette{color.White}), image.NewPaletted(image.Rect(0, 0, 20, 15), color.Palette{color.Black})}, Delay: []int{2, 2}}); err != nil {
		t.Fatal(err)
	}
	images = append(images, struct {
		name        string
		data        []byte
		contentType string
	}{"badge.gif", gifData.Bytes(), "image/gif"})
	for i, item := range images {
		rec := upload(item.name, item.data, token)
		if rec.Code != 200 {
			t.Fatalf("upload %s: %d %s", item.name, rec.Code, rec.Body.String())
		}
		var body struct {
			Result struct {
				URL string `json:"url"`
			} `json:"result"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		assetURL := body.Result.URL
		if !strings.HasPrefix(assetURL, "/static/badges/normalized/") {
			t.Fatal(assetURL)
		}
		public := httptest.NewRecorder()
		router.ServeHTTP(public, httptest.NewRequest(http.MethodGet, assetURL, nil))
		if public.Code != 200 || !strings.HasPrefix(public.Header().Get("Content-Type"), item.contentType) {
			t.Fatalf("static %d %v", public.Code, public.Header())
		}
		if item.contentType == "image/gif" {
			animated, err := gif.DecodeAll(bytes.NewReader(public.Body.Bytes()))
			if err != nil || len(animated.Image) != 2 || animated.Delay[0] != 2 || animated.Delay[1] != 2 {
				t.Fatal("normalized GIF lost playback")
			}
		}
		save := serveAuthSecurityJSON(router, "POST", "/api/admin/badge-save", fmt.Sprintf(`{"code":"asset-badge-%d","name":"Asset","type":"custom","iconUrl":%q,"isEnabled":true}`, i, assetURL), token)
		if got := decodeContractEnvelope(t, save); got.Code != 0 {
			t.Fatalf("save: %s", save.Body.String())
		}
		var usage fileUsage.Entity
		if err := conn.Where("file_name = ?", strings.TrimPrefix(assetURL, "/static/")).First(&usage).Error; err != nil {
			t.Fatal(err)
		}
		if usage.ExpiresAt != nil {
			t.Fatal("saved badge expires")
		}
		removed, err := decorationassetservice.CleanupAssets(t.Context(), time.Now().Add(25*time.Hour), 100)
		if err != nil || removed != 0 {
			t.Fatalf("saved asset GC: %d %v", removed, err)
		}
		for _, legacy := range []string{"/static/badges/contributor.svg", "https://cdn.example.com/badge.gif"} {
			rec := serveAuthSecurityJSON(router, "POST", "/api/admin/badge-save", fmt.Sprintf(`{"code":"asset-badge-%d","name":"Asset","type":"custom","iconUrl":%q}`, i, legacy), token)
			if got := decodeContractEnvelope(t, rec); got.Code != 0 {
				t.Fatal(rec.Body.String())
			}
		}
		removed, err = decorationassetservice.CleanupAssets(t.Context(), time.Now(), 100)
		if err != nil || removed != 1 {
			t.Fatalf("replaced asset GC: %d %v", removed, err)
		}
	}
	// Shared uploads stay live until the final badge is deleted.
	rec := upload("shared.gif", gifData.Bytes(), token)
	var shared struct {
		Result struct {
			URL string `json:"url"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &shared); err != nil || rec.Code != 200 {
		t.Fatal("shared upload failed")
	}
	for _, code := range []string{"asset-badge-shared-a", "asset-badge-shared-b"} {
		rec := serveAuthSecurityJSON(router, "POST", "/api/admin/badge-save", fmt.Sprintf(`{"code":%q,"name":"Shared","type":"custom","iconUrl":%q}`, code, shared.Result.URL), token)
		if decodeContractEnvelope(t, rec).Code != 0 {
			t.Fatal(rec.Body.String())
		}
	}
	for i, code := range []string{"asset-badge-shared-a", "asset-badge-shared-b"} {
		rec := serveAuthSecurityJSON(router, "POST", "/api/admin/badge-delete", fmt.Sprintf(`{"code":%q}`, code), token)
		if decodeContractEnvelope(t, rec).Code != 0 {
			t.Fatal(rec.Body.String())
		}
		removed, err := decorationassetservice.CleanupAssets(t.Context(), time.Now(), 100)
		if err != nil || removed != i {
			t.Fatalf("shared asset cleanup: %d %v, expected %d", removed, err, i)
		}
	}
	rec = serveAuthSecurityJSON(router, "POST", "/api/admin/badge-save", `{"code":"asset-badge-forged","name":"Forged","type":"custom","iconUrl":"/static/badges/11111111-1111-1111-1111-111111111111.png"}`, token)
	if decodeContractEnvelope(t, rec).Code == 0 {
		t.Fatal("saved nonexistent upload")
	}
	var forgedCount int64
	conn.Model(&badges.Entity{}).Where("code = ?", "asset-badge-forged").Count(&forgedCount)
	if forgedCount != 0 {
		t.Fatal("failed asset binding was not rolled back")
	}

}
