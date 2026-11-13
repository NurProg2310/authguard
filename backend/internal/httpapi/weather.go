package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/authguard/backend/internal/weather"
)

func (h *handlers) weatherForecast(c *gin.Context) {
	lat, err1 := strconv.ParseFloat(c.Query("lat"), 64)
	lon, err2 := strconv.ParseFloat(c.Query("lon"), 64)
	if err1 != nil || err2 != nil || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		fail(c, http.StatusBadRequest, "BAD_REQUEST", "lat and lon must be valid coordinates")
		return
	}
	f, err := h.Weather.Forecast(c.Request.Context(), lat, lon)
	if err != nil {
		h.weatherError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"forecast": f})
}

func (h *handlers) weatherSearch(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if n := utf8.RuneCountInString(q); n < 2 || n > 80 {
		c.JSON(http.StatusOK, gin.H{"places": []weather.Place{}})
		return
	}
	lang := strings.ToLower(c.DefaultQuery("lang", "en"))
	if len(lang) != 2 {
		lang = "en"
	}
	places, err := h.Weather.Search(c.Request.Context(), strings.ToLower(q), lang)
	if err != nil {
		h.weatherError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"places": places})
}

func (h *handlers) weatherError(c *gin.Context, err error) {
	if errors.Is(err, weather.ErrUpstream) {
		h.Log.Warn("weather upstream", "error", err)
		fail(c, http.StatusBadGateway, "WEATHER_UNAVAILABLE", "Weather is temporarily unavailable")
		return
	}
	h.internal(c, err)
}
