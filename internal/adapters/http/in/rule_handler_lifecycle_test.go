// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package in

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"tracer/pkg/constant"
	"tracer/pkg/model"
)

func TestActivateRuleHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)

	app := fiber.New()
	ruleID := uuid.New()

	rule := &model.Rule{
		ID:     ruleID,
		Name:   "Test Rule",
		Status: model.RuleStatusActive,
	}

	mockService := NewMockRuleService(ctrl)

	mockService.EXPECT().
		ActivateRule(gomock.Any(), ruleID).
		Return(rule, nil)

	handler := NewHandler(mockService)
	app.Post("/v1/rules/:id/activate", handler.ActivateRule)

	req := httptest.NewRequest(http.MethodPost, "/v1/rules/"+ruleID.String()+"/activate", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body model.Rule
	err = json.NewDecoder(resp.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, ruleID, body.ID)
	assert.Equal(t, model.RuleStatusActive, body.Status)
}

func TestActivateRuleHandler_InvalidUUID(t *testing.T) {
	ctrl := gomock.NewController(t)

	app := fiber.New()

	mockService := NewMockRuleService(ctrl)

	handler := NewHandler(mockService)
	app.Post("/v1/rules/:id/activate", handler.ActivateRule)

	req := httptest.NewRequest(http.MethodPost, "/v1/rules/invalid-uuid/activate", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestActivateRuleHandler_ServiceError(t *testing.T) {
	ctrl := gomock.NewController(t)

	app := fiber.New()
	ruleID := uuid.New()

	mockService := NewMockRuleService(ctrl)

	mockService.EXPECT().
		ActivateRule(gomock.Any(), ruleID).
		Return(nil, model.NewInvalidTransitionError(model.RuleStatusDeleted, model.RuleStatusActive))

	handler := NewHandler(mockService)
	app.Post("/v1/rules/:id/activate", handler.ActivateRule)

	req := httptest.NewRequest(http.MethodPost, "/v1/rules/"+ruleID.String()+"/activate", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestActivateRuleHandler_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)

	app := fiber.New()
	ruleID := uuid.New()

	mockService := NewMockRuleService(ctrl)

	mockService.EXPECT().
		ActivateRule(gomock.Any(), ruleID).
		Return(nil, constant.ErrRuleNotFound)

	handler := NewHandler(mockService)
	app.Post("/v1/rules/:id/activate", handler.ActivateRule)

	req := httptest.NewRequest(http.MethodPost, "/v1/rules/"+ruleID.String()+"/activate", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestActivateRuleHandler_InternalError(t *testing.T) {
	ctrl := gomock.NewController(t)

	app := fiber.New()
	ruleID := uuid.New()

	mockService := NewMockRuleService(ctrl)

	mockService.EXPECT().
		ActivateRule(gomock.Any(), ruleID).
		Return(nil, errors.New("database connection failed"))

	handler := NewHandler(mockService)
	app.Post("/v1/rules/:id/activate", handler.ActivateRule)

	req := httptest.NewRequest(http.MethodPost, "/v1/rules/"+ruleID.String()+"/activate", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestDeactivateRuleHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)

	app := fiber.New()
	ruleID := uuid.New()

	rule := &model.Rule{
		ID:     ruleID,
		Name:   "Test Rule",
		Status: model.RuleStatusInactive,
	}

	mockService := NewMockRuleService(ctrl)

	mockService.EXPECT().
		DeactivateRule(gomock.Any(), ruleID).
		Return(rule, nil)

	handler := NewHandler(mockService)
	app.Post("/v1/rules/:id/deactivate", handler.DeactivateRule)

	req := httptest.NewRequest(http.MethodPost, "/v1/rules/"+ruleID.String()+"/deactivate", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body model.Rule
	err = json.NewDecoder(resp.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, ruleID, body.ID)
	assert.Equal(t, model.RuleStatusInactive, body.Status)
}

func TestDeactivateRuleHandler_InvalidUUID(t *testing.T) {
	ctrl := gomock.NewController(t)

	app := fiber.New()

	mockService := NewMockRuleService(ctrl)

	handler := NewHandler(mockService)
	app.Post("/v1/rules/:id/deactivate", handler.DeactivateRule)

	req := httptest.NewRequest(http.MethodPost, "/v1/rules/invalid-uuid/deactivate", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestDeactivateRuleHandler_ServiceError(t *testing.T) {
	ctrl := gomock.NewController(t)

	app := fiber.New()
	ruleID := uuid.New()

	mockService := NewMockRuleService(ctrl)

	mockService.EXPECT().
		DeactivateRule(gomock.Any(), ruleID).
		Return(nil, model.NewInvalidTransitionError(model.RuleStatusDeleted, model.RuleStatusInactive))

	handler := NewHandler(mockService)
	app.Post("/v1/rules/:id/deactivate", handler.DeactivateRule)

	req := httptest.NewRequest(http.MethodPost, "/v1/rules/"+ruleID.String()+"/deactivate", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestDeactivateRuleHandler_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)

	app := fiber.New()
	ruleID := uuid.New()

	mockService := NewMockRuleService(ctrl)

	mockService.EXPECT().
		DeactivateRule(gomock.Any(), ruleID).
		Return(nil, constant.ErrRuleNotFound)

	handler := NewHandler(mockService)
	app.Post("/v1/rules/:id/deactivate", handler.DeactivateRule)

	req := httptest.NewRequest(http.MethodPost, "/v1/rules/"+ruleID.String()+"/deactivate", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestDeactivateRuleHandler_InternalError(t *testing.T) {
	ctrl := gomock.NewController(t)

	app := fiber.New()
	ruleID := uuid.New()

	mockService := NewMockRuleService(ctrl)

	mockService.EXPECT().
		DeactivateRule(gomock.Any(), ruleID).
		Return(nil, errors.New("database connection failed"))

	handler := NewHandler(mockService)
	app.Post("/v1/rules/:id/deactivate", handler.DeactivateRule)

	req := httptest.NewRequest(http.MethodPost, "/v1/rules/"+ruleID.String()+"/deactivate", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestDeleteRuleHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)

	app := fiber.New()
	ruleID := uuid.New()

	mockService := NewMockRuleService(ctrl)

	mockService.EXPECT().
		DeleteRule(gomock.Any(), ruleID).
		Return(nil)

	handler := NewHandler(mockService)
	app.Delete("/v1/rules/:id", handler.DeleteRule)

	req := httptest.NewRequest(http.MethodDelete, "/v1/rules/"+ruleID.String(), nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestDeleteRuleHandler_InvalidUUID(t *testing.T) {
	ctrl := gomock.NewController(t)

	app := fiber.New()

	mockService := NewMockRuleService(ctrl)

	handler := NewHandler(mockService)
	app.Delete("/v1/rules/:id", handler.DeleteRule)

	req := httptest.NewRequest(http.MethodDelete, "/v1/rules/invalid-uuid", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestDeleteRuleHandler_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)

	app := fiber.New()
	ruleID := uuid.New()

	mockService := NewMockRuleService(ctrl)

	mockService.EXPECT().
		DeleteRule(gomock.Any(), ruleID).
		Return(constant.ErrRuleNotFound)

	handler := NewHandler(mockService)
	app.Delete("/v1/rules/:id", handler.DeleteRule)

	req := httptest.NewRequest(http.MethodDelete, "/v1/rules/"+ruleID.String(), nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestDeleteRuleHandler_InvalidTransition(t *testing.T) {
	ctrl := gomock.NewController(t)

	app := fiber.New()
	ruleID := uuid.New()

	mockService := NewMockRuleService(ctrl)

	mockService.EXPECT().
		DeleteRule(gomock.Any(), ruleID).
		Return(model.NewInvalidTransitionError(model.RuleStatusActive, model.RuleStatusDeleted))

	handler := NewHandler(mockService)
	app.Delete("/v1/rules/:id", handler.DeleteRule)

	req := httptest.NewRequest(http.MethodDelete, "/v1/rules/"+ruleID.String(), nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestDeleteRuleHandler_InternalError(t *testing.T) {
	ctrl := gomock.NewController(t)

	app := fiber.New()
	ruleID := uuid.New()

	mockService := NewMockRuleService(ctrl)

	mockService.EXPECT().
		DeleteRule(gomock.Any(), ruleID).
		Return(errors.New("database connection failed"))

	handler := NewHandler(mockService)
	app.Delete("/v1/rules/:id", handler.DeleteRule)

	req := httptest.NewRequest(http.MethodDelete, "/v1/rules/"+ruleID.String(), nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}
