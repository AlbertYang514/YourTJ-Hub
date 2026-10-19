package api

import (
	"errors"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/http/controllers/component"
	"github.com/YourTongji/YourTJ-Hub/apps/gooseforum/app/service/decorationassetservice"
	"github.com/gin-gonic/gin"
)

func UploadDecorationAsset(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	userID := c.GetUint64("userId")
	policy, failure := resolveImageUploadPolicy(userID)
	if failure != nil {
		c.JSON(failure.Status, failure.Data)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, policy.MaxSize+1024*1024)
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, component.FailDataCode(component.MessageUploadFileMissing, nil))
		return
	}
	ext := strings.ToLower(path.Ext(file.Filename))
	if ext != ".png" && ext != ".webp" {
		c.JSON(http.StatusBadRequest, component.FailDataCode(component.MessageUploadUnsupportedExt, nil))
		return
	}
	reportedType := file.Header.Get("Content-Type")
	if reportedType == "application/octet-stream" {
		reportedType = ""
	}
	_, failure = policy.Validate(file.Filename, file.Size, reportedType)
	if failure != nil {
		c.JSON(failure.Status, failure.Data)
		return
	}
	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, component.FailDataCode(component.MessageUploadReadFailed, nil))
		return
	}
	defer func() { _ = src.Close() }()
	data, err := io.ReadAll(io.LimitReader(src, policy.MaxSize+1))
	if err != nil {
		c.JSON(http.StatusInternalServerError, component.FailDataCode(component.MessageUploadContentReadFailed, nil))
		return
	}
	if int64(len(data)) > policy.MaxSize {
		c.JSON(http.StatusBadRequest, component.FailDataCode(component.MessageUploadFileTooLarge, nil))
		return
	}
	url, err := decorationassetservice.UploadAsset(c.Request.Context(), userID, data, file.Filename)
	if err != nil {
		if errors.Is(err, decorationassetservice.ErrInvalid) {
			c.JSON(http.StatusBadRequest, component.FailDataCode(component.MessageUploadInvalidImage, nil))
			return
		}
		c.JSON(http.StatusInternalServerError, component.FailDataCode(component.MessageUploadSaveFailed, nil))
		return
	}
	c.JSON(http.StatusOK, component.SuccessDataCode(gin.H{"url": url}, component.MessageUploadSuccess, nil))
}
