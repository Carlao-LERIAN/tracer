// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package http

import (
	"github.com/gofiber/fiber/v2"

	"tracer/pkg"
	"tracer/pkg/constant"
)

// WithError returns an error with the given status code and message.
func WithError(c *fiber.Ctx, err error) error {
	switch e := err.(type) {
	case pkg.EntityNotFoundError:
		return NotFound(c, e.Code, e.Title, e.Message)
	case pkg.EntityConflictError:
		return Conflict(c, e.Code, e.Title, e.Message)
	case pkg.ValidationError:
		return BadRequest(c, pkg.ValidationKnownFieldsError{
			Code:    e.Code,
			Title:   e.Title,
			Message: e.Message,
			Fields:  nil,
		})
	case pkg.UnprocessableOperationError:
		return UnprocessableEntity(c, e.Code, e.Title, e.Message)
	case pkg.UnauthorizedError:
		return Unauthorized(c, e.Code, e.Title, e.Message)
	case pkg.ForbiddenError:
		return Forbidden(c, e.Code, e.Title, e.Message)
	case pkg.ValidationKnownFieldsError, pkg.ValidationUnknownFieldsError:
		return BadRequest(c, e)
	case pkg.ResponseError:
		return JSONResponseError(c, e)
	default:
		// ValidateInternalError always returns an InternalServerError
		iErr, ok := pkg.ValidateInternalError(err, "").(pkg.InternalServerError)
		if !ok {
			// Fallback uses centralized constants for consistency
			return InternalServerError(c, constant.ErrInternalServer.Error(), "Internal Server Error",
				"The server encountered an unexpected error. Please try again later or contact support.")
		}

		return InternalServerError(c, iErr.Code, iErr.Title, iErr.Message)
	}
}
