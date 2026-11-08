package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// PushHandler exposes the push device registry (self-service, both tiers)
// and the admin test push. The tier of a self-service route is fixed by the
// router it is mounted on and handed to the handler at registration; it is
// never read from the request.
type PushHandler struct {
	devices repository.DeviceTokenRepository
	sender  iface.PushSender
	now     func() time.Time
}

func NewPushHandler(devices repository.DeviceTokenRepository, sender iface.PushSender) *PushHandler {
	return &PushHandler{devices: devices, sender: sender, now: time.Now}
}

// NotificationPushDevice is the public projection of a device row. It never
// carries the push token.
type NotificationPushDevice struct {
	DeviceID   string    `json:"deviceId"`
	Platform   string    `json:"platform"`
	AppVersion string    `json:"appVersion,omitempty"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
}

func pushDeviceView(d *models.DeviceToken) NotificationPushDevice {
	return NotificationPushDevice{
		DeviceID: d.DeviceID, Platform: d.Platform, AppVersion: d.AppVersion,
		Active: d.Active, CreatedAt: d.CreatedAt, LastSeenAt: d.LastSeenAt,
	}
}

type NotificationPushRegisterInput struct {
	DeviceID string `path:"deviceId" minLength:"1" maxLength:"128" pattern:"^[A-Za-z0-9._:-]+$" doc:"Stable id of the device, chosen by the app"`
	Body     struct {
		Token      string `json:"token" minLength:"1" maxLength:"4096" doc:"Provider push token of the device"`
		Platform   string `json:"platform" enum:"ios,android,web" doc:"Device platform"`
		AppVersion string `json:"appVersion,omitempty" maxLength:"64" doc:"App version, informational"`
	}
}

type NotificationPushDeviceOutput struct {
	Body NotificationPushDevice
}

type NotificationPushDeviceIDInput struct {
	DeviceID string `path:"deviceId" minLength:"1" maxLength:"128" pattern:"^[A-Za-z0-9._:-]+$" doc:"Device id"`
}

type NotificationPushDevicesOutput struct {
	Body struct {
		Devices []NotificationPushDevice `json:"devices"`
	}
}

type NotificationPushNoContent struct{}

// caller returns the authenticated user, or the 401 every route answers with.
func pushCaller(ctx context.Context) (string, error) {
	u, _ := ctxauth.GetUserUUID(ctx)
	if u == "" {
		return "", huma.Error401Unauthorized("authentication required")
	}
	return u, nil
}

func (h *PushHandler) register(tier string) func(context.Context, *NotificationPushRegisterInput) (*NotificationPushDeviceOutput, error) {
	return func(ctx context.Context, in *NotificationPushRegisterInput) (*NotificationPushDeviceOutput, error) {
		user, err := pushCaller(ctx)
		if err != nil {
			return nil, err
		}
		sid, ok := ctxauth.GetSessionID(ctx)
		if !ok {
			return nil, errcode.Unauthorized(errcode.NotificationPushSessionMissing,
				"The access token has no session id; sign in again to register this device.")
		}
		row, err := h.devices.Upsert(ctx, &models.DeviceToken{
			Tier: tier, UserUUID: user, DeviceID: in.DeviceID, SessionID: sid,
			Token: in.Body.Token, Platform: in.Body.Platform, AppVersion: in.Body.AppVersion,
		}, h.now().UTC())
		if err != nil {
			return nil, huma.Error500InternalServerError("device registration failed")
		}
		return &NotificationPushDeviceOutput{Body: pushDeviceView(row)}, nil
	}
}

func (h *PushHandler) unregister(tier string) func(context.Context, *NotificationPushDeviceIDInput) (*NotificationPushNoContent, error) {
	return func(ctx context.Context, in *NotificationPushDeviceIDInput) (*NotificationPushNoContent, error) {
		user, err := pushCaller(ctx)
		if err != nil {
			return nil, err
		}
		found, err := h.devices.Disable(ctx, tier, user, in.DeviceID, models.DeviceDisabledLogout, h.now().UTC())
		if err != nil {
			return nil, huma.Error500InternalServerError("device removal failed")
		}
		if !found {
			// Idempotent: a row that is already disabled (a retried logout,
			// or one FCM or the cleanup retired first) is a success and
			// keeps its original reason. Only an id that never existed is 404.
			rows, err := h.devices.ListForUser(ctx, tier, user)
			if err != nil {
				return nil, huma.Error500InternalServerError("device removal failed")
			}
			exists := false
			for _, r := range rows {
				if r.DeviceID == in.DeviceID {
					exists = true
					break
				}
			}
			if !exists {
				return nil, errcode.NotFound(errcode.NotificationPushDeviceNotFound, "No push device has that id.")
			}
		}
		return &NotificationPushNoContent{}, nil
	}
}

func (h *PushHandler) list(tier string) func(context.Context, *struct{}) (*NotificationPushDevicesOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*NotificationPushDevicesOutput, error) {
		user, err := pushCaller(ctx)
		if err != nil {
			return nil, err
		}
		rows, err := h.devices.ListForUser(ctx, tier, user)
		if err != nil {
			return nil, huma.Error500InternalServerError("device list failed")
		}
		out := &NotificationPushDevicesOutput{}
		out.Body.Devices = make([]NotificationPushDevice, 0, len(rows))
		for i := range rows {
			out.Body.Devices = append(out.Body.Devices, pushDeviceView(&rows[i]))
		}
		return out, nil
	}
}

// RegisterDeviceRoutes mounts the self-service device routes for one tier.
// opPrefix ("" or "client-") keeps the OpenAPI operation ids unique across
// the two mounts.
func (h *PushHandler) RegisterDeviceRoutes(api huma.API, tier, opPrefix string) {
	sec := []map[string][]string{{"bearerAuth": {}}}
	tags := []string{"Notifications"}
	huma.Register(api, huma.Operation{
		OperationID: opPrefix + "notifications-push-register-device", Method: http.MethodPut,
		Path: "/v1/notifications/push/devices/{deviceId}", Summary: "Register or refresh a push device",
		Tags: tags, Security: sec,
	}, h.register(tier))
	huma.Register(api, huma.Operation{
		OperationID: opPrefix + "notifications-push-remove-device", Method: http.MethodDelete,
		Path: "/v1/notifications/push/devices/{deviceId}", Summary: "Unregister a push device",
		Description: "Idempotent: 204 whenever the device row exists for this user, including one already disabled (its original reason is kept); 404 only when the id was never registered.",
		Tags:        tags, Security: sec, DefaultStatus: http.StatusNoContent,
	}, h.unregister(tier))
	huma.Register(api, huma.Operation{
		OperationID: opPrefix + "notifications-push-list-devices", Method: http.MethodGet,
		Path: "/v1/notifications/push/devices", Summary: "List my push devices",
		Tags: tags, Security: sec,
	}, h.list(tier))
}

type NotificationPushTestInput struct {
	Body struct {
		Tier     string `json:"tier" doc:"operator or client"`
		UserUUID string `json:"userUuid" minLength:"1" doc:"Recipient user"`
		Title    string `json:"title,omitempty" doc:"Defaults to \"Orkestra test\""`
		Body     string `json:"body,omitempty" doc:"Defaults to \"Test push notification\""`
	}
}

type NotificationPushUserResult struct {
	UserUUID  string `json:"userUuid"`
	Status    string `json:"status"`
	Attempted int    `json:"attempted"`
	Delivered int    `json:"delivered"`
	Disabled  int    `json:"disabled"`
	Error     string `json:"error,omitempty"`
}

type NotificationPushTestOutput struct {
	Body struct {
		Users []NotificationPushUserResult `json:"users"`
	}
}

// SendTest pushes a test message to one user's devices.
func (h *PushHandler) SendTest(ctx context.Context, in *NotificationPushTestInput) (*NotificationPushTestOutput, error) {
	title, body := in.Body.Title, in.Body.Body
	if title == "" {
		title = "Orkestra test"
	}
	if body == "" {
		body = "Test push notification"
	}
	res, err := h.sender.SendPush(ctx, iface.PushRequest{
		Tier: in.Body.Tier, UserUUIDs: []string{in.Body.UserUUID},
		Category: "notification.test", Type: iface.PushTypeTransactional,
		Title: title, Body: body,
	})
	if err != nil {
		switch {
		case errors.Is(err, iface.ErrPushInvalidRequest):
			return nil, errcode.UnprocessableEntity(errcode.NotificationPushInvalidRequest, "The push request is not valid: tier must be operator or client, and the title and body must fit their limits.")
		case errors.Is(err, iface.ErrPushUnavailable):
			return nil, errcode.ServiceUnavailable(errcode.NotificationPushUnavailable, "Push delivery is not available right now.")
		default:
			return nil, huma.Error500InternalServerError("push failed")
		}
	}
	out := &NotificationPushTestOutput{}
	out.Body.Users = make([]NotificationPushUserResult, 0, len(res.Users))
	for _, u := range res.Users {
		out.Body.Users = append(out.Body.Users, NotificationPushUserResult{
			UserUUID: u.UserUUID, Status: u.Status, Attempted: u.Attempted,
			Delivered: u.Delivered, Disabled: u.Disabled, Error: u.Error,
		})
	}
	return out, nil
}

// RegisterAdminTestRoute mounts the admin test push. The caller gates it
// with RequireSystemPermission("notification.test").
func (h *PushHandler) RegisterAdminTestRoute(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "notifications-push-test", Method: http.MethodPost,
		Path: "/v1/notifications/push/test", Summary: "Send a test push notification to one user",
		Tags: []string{"Notifications"}, Security: []map[string][]string{{"bearerAuth": {}}},
	}, h.SendTest)
}
