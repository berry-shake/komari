package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/komari-monitor/komari/pkg/ddns"
	"github.com/komari-monitor/komari/web/api"
)

// RegisterDDNSRoutes is only mounted below the existing administrator guard.
func RegisterDDNSRoutes(group *gin.RouterGroup, service *ddns.Service) {
	g := group.Group("/ddns")
	g.GET("/settings", func(c *gin.Context) { v, e := service.Settings(); ddnsRespond(c, v, e) })
	g.PUT("/settings", func(c *gin.Context) {
		var in ddns.SettingsInput
		if !ddnsBind(c, &in) {
			return
		}
		e := service.SaveSettings(in)
		ddnsRespond(c, nil, e)
	})
	g.GET("/records", func(c *gin.Context) { v, e := service.Records(); ddnsRespond(c, v, e) })
	g.POST("/records", func(c *gin.Context) {
		var in ddns.RecordInput
		if !ddnsBind(c, &in) {
			return
		}
		v, e := service.SaveRecord("", in)
		ddnsRespond(c, v, e)
	})
	g.PUT("/records/:id", func(c *gin.Context) {
		var in ddns.RecordInput
		if !ddnsBind(c, &in) {
			return
		}
		v, e := service.SaveRecord(c.Param("id"), in)
		ddnsRespond(c, v, e)
	})
	g.DELETE("/records/:id", func(c *gin.Context) { ddnsRespond(c, nil, service.DeleteRecord(c.Param("id"))) })
	g.GET("/nodes", func(c *gin.Context) { v, e := service.Nodes(); ddnsRespond(c, v, e) })
	g.POST("/sync", func(c *gin.Context) { v, e := service.Sync(c.Request.Context()); ddnsRespond(c, v, e) })
	g.GET("/logs", func(c *gin.Context) {
		limit, _ := strconv.Atoi(c.Query("limit"))
		v, e := service.Logs(c.Query("record"), c.Query("action"), limit)
		ddnsRespond(c, v, e)
	})
	g.DELETE("/logs", func(c *gin.Context) { ddnsRespond(c, nil, service.ClearLogs()) })
}
func ddnsBind(c *gin.Context, dest any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 65536)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		api.RespondError(c, http.StatusBadRequest, "DDNS 请求 JSON 无效或超过 64 KiB")
		return false
	}
	if decoder.Decode(new(any)) != io.EOF {
		api.RespondError(c, http.StatusBadRequest, "请求必须只包含一个 JSON 对象")
		return false
	}
	return true
}
func ddnsRespond(c *gin.Context, value any, err error) {
	if err == nil {
		api.RespondSuccess(c, value)
		return
	}
	code, message := http.StatusInternalServerError, "DDNS 数据操作失败"
	var invalid *ddns.ValidationError
	switch {
	case errors.As(err, &invalid):
		code, message = http.StatusBadRequest, invalid.Error()
	case errors.Is(err, ddns.ErrBusy):
		code, message = http.StatusConflict, err.Error()
	case errors.Is(err, ddns.ErrNotFound):
		code, message = http.StatusNotFound, err.Error()
	}
	api.RespondError(c, code, message)
}
