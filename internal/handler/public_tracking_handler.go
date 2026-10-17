package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"ridematch-backend/internal/service"
	"ridematch-backend/internal/utils"
)

// PublicTrackingHandler serves the no-login "someone shared their live
// location with you" experience: a small self-contained HTML page (GET
// /t/:token) that polls a JSON status endpoint (GET /api/v1/track/:token)
// every few seconds and draws the trip on a map. Deliberately outside
// every auth/role middleware — the whole point of a share link is that
// the person opening it never signs in.
type PublicTrackingHandler struct {
	trips *service.TripService
}

// NewPublicTrackingHandler constructs a PublicTrackingHandler.
func NewPublicTrackingHandler(trips *service.TripService) *PublicTrackingHandler {
	return &PublicTrackingHandler{trips: trips}
}

// Status godoc
//
//	@Summary		Poll a shared trip's live status (no login required)
//	@Description	Resolves a live-location share token into a trimmed, public view of the trip — status, pickup/destination, and the driver's current position while matched/picked up. No phone number, email, or pricing is ever included.
//	@Tags			Public tracking
//	@Produce		json
//	@Param			token	path		string	true	"Share token from POST /trips/{id}/share-location"
//	@Success		200	{object}	utils.APIResponse{data=dto.PublicTrackingResponse}
//	@Failure		404	{object}	utils.APIResponse	"Link is invalid or has expired"
//	@Router			/track/{token} [get]
func (h *PublicTrackingHandler) Status(c *gin.Context) {
	resp, err := h.trips.GetPublicTracking(c.Request.Context(), c.Param("token"))
	if err != nil {
		if err == utils.ErrInvalidShareToken {
			utils.Fail(c, http.StatusNotFound, err.Error())
			return
		}
		utils.Fail(c, http.StatusInternalServerError, "something went wrong, please try again")
		return
	}
	utils.Success(c, http.StatusOK, "tracking status loaded", resp)
}

// View serves the public, no-login HTML tracking page at GET /t/:token.
// The page itself carries no trip data server-side-rendered into it (so
// this handler never needs to validate the token) — it just bootstraps a
// small script that polls Status above and draws the result with
// Leaflet/OpenStreetMap (no API key needed, unlike Google Maps).
func (h *PublicTrackingHandler) View(c *gin.Context) {
	token := c.Param("token")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(fmt.Sprintf(trackingPageTemplate, token)))
}

const trackingPageTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1" />
<title>Live trip tracking — MakeWeRide</title>
<link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css" />
<style>
  :root { --brand: #F5570D; --ink: #1C1917; --ink-500: #78716C; --sand: #FBF9F6; }
  * { box-sizing: border-box; }
  html, body { margin: 0; padding: 0; height: 100%%; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: var(--sand); }
  #map { position: absolute; inset: 0; }
  .panel {
    position: absolute; left: 12px; right: 12px; top: 12px; z-index: 1000;
    background: rgba(255,255,255,0.97); border-radius: 18px; padding: 14px 16px;
    box-shadow: 0 6px 24px rgba(28,25,23,0.14);
  }
  .brand { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; }
  .brand .dot { width: 9px; height: 9px; border-radius: 999px; background: var(--brand); }
  .brand span { font-weight: 700; font-size: 13px; color: var(--ink); letter-spacing: 0.2px; }
  #status-line { font-size: 14px; color: var(--ink); font-weight: 600; }
  #sub-line { font-size: 12.5px; color: var(--ink-500); margin-top: 2px; }
  .error-box {
    position: absolute; inset: 0; display: none; flex-direction: column; align-items: center;
    justify-content: center; text-align: center; padding: 32px; background: var(--sand);
  }
  .error-box h1 { font-size: 17px; color: var(--ink); margin: 0 0 6px; }
  .error-box p { font-size: 14px; color: var(--ink-500); margin: 0; max-width: 320px; }
</style>
</head>
<body>
  <div id="map"></div>
  <div class="panel">
    <div class="brand"><span class="dot"></span><span>KINEORIDE · LIVE TRACKING</span></div>
    <div id="status-line">Loading trip…</div>
    <div id="sub-line"></div>
  </div>
  <div class="error-box" id="error-box">
    <h1>This link isn't available</h1>
    <p id="error-message">It may have expired, or the trip has ended.</p>
  </div>

  <script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"></script>
  <script>
    var TOKEN = %q;
    var map = L.map('map', { zoomControl: true, attributionControl: true }).setView([6.5244, 3.3792], 12);
    L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
      maxZoom: 19,
      attribution: '&copy; OpenStreetMap contributors'
    }).addTo(map);

    var pickupIcon = L.divIcon({ className: '', html: '<div style="width:14px;height:14px;border-radius:999px;background:#F5570D;border:3px solid white;box-shadow:0 2px 6px rgba(0,0,0,0.3)"></div>', iconSize: [14,14] });
    var destIcon = L.divIcon({ className: '', html: '<div style="width:14px;height:14px;border-radius:999px;background:#289D93;border:3px solid white;box-shadow:0 2px 6px rgba(0,0,0,0.3)"></div>', iconSize: [14,14] });
    var carIcon = L.divIcon({
      className: '',
      html: '<div style="width:30px;height:30px;border-radius:10px;background:#F5570D;border:2px solid #B6350A;box-shadow:0 3px 10px rgba(0,0,0,0.35);display:flex;align-items:center;justify-content:center;color:white;font-size:16px;">&#128663;</div>',
      iconSize: [30, 30],
    });

    var pickupMarker = null, destMarker = null, carMarker = null, fitted = false, polling = true;

    var STATUS_LABEL = {
      requested: 'Looking for a driver…',
      matched: 'Driver is on the way to pickup',
      picked_up: 'Trip in progress',
      completed: 'Trip completed',
      cancelled: 'Trip cancelled',
      expired: 'Trip expired',
    };

    function setMarker(ref, latlng, icon) {
      if (ref.m) { ref.m.setLatLng(latlng); return ref.m; }
      ref.m = L.marker(latlng, { icon: icon }).addTo(map);
      return ref.m;
    }

    var refs = { pickup: {}, dest: {}, car: {} };

    function render(data) {
      document.getElementById('status-line').textContent = STATUS_LABEL[data.status] || data.status;
      var sub = [];
      if (data.vehicle_type) sub.push(data.vehicle_type.charAt(0).toUpperCase() + data.vehicle_type.slice(1));
      if (data.driver_name) sub.push(data.driver_name);
      if (data.plate_number) sub.push(data.plate_number);
      document.getElementById('sub-line').textContent = sub.join(' · ');

      var pickupLatLng = [data.pickup_lat, data.pickup_lng];
      var destLatLng = [data.destination_lat, data.destination_lng];
      setMarker(refs.pickup, pickupLatLng, pickupIcon);
      setMarker(refs.dest, destLatLng, destIcon);

      var points = [pickupLatLng, destLatLng];
      if (data.latitude != null && data.longitude != null) {
        var carLatLng = [data.latitude, data.longitude];
        setMarker(refs.car, carLatLng, carIcon);
        points.push(carLatLng);
      } else if (refs.car.m) {
        map.removeLayer(refs.car.m);
        refs.car.m = null;
      }

      if (!fitted) {
        map.fitBounds(points, { padding: [48, 48] });
        fitted = true;
      }

      if (data.status === 'completed' || data.status === 'cancelled' || data.status === 'expired') {
        polling = false;
      }
    }

    function showError(message) {
      document.getElementById('error-box').style.display = 'flex';
      document.getElementById('error-message').textContent = message || 'It may have expired, or the trip has ended.';
      polling = false;
    }

    function poll() {
      if (!polling) return;
      fetch('/api/v1/track/' + encodeURIComponent(TOKEN))
        .then(function (r) {
          if (!r.ok) { return r.json().then(function (body) { throw new Error(body.error || 'Not available'); }); }
          return r.json();
        })
        .then(function (body) { render(body.data); setTimeout(poll, 4000); })
        .catch(function (err) { showError(err.message); });
    }

    poll();
  </script>
</body>
</html>`
