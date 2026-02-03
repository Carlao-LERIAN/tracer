// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

// Package cel provides CEL expression compilation and evaluation.
package cel

import (
	libOtel "github.com/LerianStudio/lib-commons/v2/commons/opentelemetry"
	"go.opentelemetry.io/otel/trace"
)

// recordSpanError records an error on the span using lib-commons wrapper.
// Per Ring standards, all span error handling must use lib-commons methods.
func recordSpanError(span *trace.Span, message string, err error) {
	libOtel.HandleSpanError(span, message, err)
}
