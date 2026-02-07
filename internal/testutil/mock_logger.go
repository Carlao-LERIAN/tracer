// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

// Package testutil provides shared test utilities.
package testutil

import (
	"fmt"
	"sync"

	libLog "github.com/LerianStudio/lib-commons/v2/commons/log"
)

// LogCall represents a single logging call with its level, message, and fields.
type LogCall struct {
	Level   string
	Message string
	Fields  []any
}

// MockLogger tracks logging calls for verification in tests.
// It is safe for concurrent use.
type MockLogger struct {
	mu    sync.Mutex
	Calls []LogCall
}

// NewMockLogger creates a new MockLogger instance.
func NewMockLogger() *MockLogger {
	return &MockLogger{
		Calls: []LogCall{},
	}
}

func (m *MockLogger) Info(args ...any)                                  {}
func (m *MockLogger) Infof(format string, args ...any)                  {}
func (m *MockLogger) Infoln(args ...any)                                {}
func (m *MockLogger) Error(args ...any)                                 {}
func (m *MockLogger) Errorf(format string, args ...any)                 {}
func (m *MockLogger) Errorln(args ...any)                               {}
func (m *MockLogger) Warn(args ...any)                                  {}
func (m *MockLogger) Warnf(format string, args ...any)                  {}
func (m *MockLogger) Warnln(args ...any)                                {}
func (m *MockLogger) Debug(args ...any)                                 {}
func (m *MockLogger) Debugf(format string, args ...any)                 {}
func (m *MockLogger) Debugln(args ...any)                               {}
func (m *MockLogger) Fatal(args ...any)                                 {}
func (m *MockLogger) Fatalf(format string, args ...any)                 {}
func (m *MockLogger) Fatalln(args ...any)                               {}
func (m *MockLogger) WithDefaultMessageTemplate(s string) libLog.Logger { return m }
func (m *MockLogger) Sync() error                                       { return nil }

// WithFields returns a recorder that captures subsequent log calls with the provided fields.
func (m *MockLogger) WithFields(fields ...any) libLog.Logger {
	return &mockLoggerFieldsRecorder{parent: m, fields: fields}
}

// mockLoggerFieldsRecorder records WithFields and subsequent log calls.
type mockLoggerFieldsRecorder struct {
	parent *MockLogger
	fields []any
}

func (m *mockLoggerFieldsRecorder) record(level, msg string) {
	m.parent.mu.Lock()
	m.parent.Calls = append(m.parent.Calls, LogCall{Level: level, Message: msg, Fields: m.fields})
	m.parent.mu.Unlock()
}

func (m *mockLoggerFieldsRecorder) Info(args ...any) {
	msg := ""

	if len(args) > 0 {
		if s, ok := args[0].(string); ok {
			msg = s
		}
	}

	m.record("info", msg)
}

func (m *mockLoggerFieldsRecorder) Infof(format string, args ...any) {
	m.record("info", fmt.Sprintf(format, args...))
}

func (m *mockLoggerFieldsRecorder) Infoln(args ...any) {
	m.record("info", fmt.Sprint(args...))
}

func (m *mockLoggerFieldsRecorder) Error(args ...any) {
	msg := ""

	if len(args) > 0 {
		if s, ok := args[0].(string); ok {
			msg = s
		}
	}

	m.record("error", msg)
}

func (m *mockLoggerFieldsRecorder) Errorf(format string, args ...any) {
	m.record("error", fmt.Sprintf(format, args...))
}

func (m *mockLoggerFieldsRecorder) Errorln(args ...any) {
	m.record("error", fmt.Sprint(args...))
}

func (m *mockLoggerFieldsRecorder) Warn(args ...any) {
	msg := ""

	if len(args) > 0 {
		if s, ok := args[0].(string); ok {
			msg = s
		}
	}

	m.record("warn", msg)
}

func (m *mockLoggerFieldsRecorder) Warnf(format string, args ...any) {
	m.record("warn", fmt.Sprintf(format, args...))
}

func (m *mockLoggerFieldsRecorder) Warnln(args ...any) {
	m.record("warn", fmt.Sprint(args...))
}

func (m *mockLoggerFieldsRecorder) Debug(args ...any) {
	msg := ""

	if len(args) > 0 {
		if s, ok := args[0].(string); ok {
			msg = s
		}
	}

	m.record("debug", msg)
}

func (m *mockLoggerFieldsRecorder) Debugf(format string, args ...any) {
	m.record("debug", fmt.Sprintf(format, args...))
}

func (m *mockLoggerFieldsRecorder) Debugln(args ...any) {
	m.record("debug", fmt.Sprint(args...))
}

func (m *mockLoggerFieldsRecorder) Fatal(args ...any) {
	msg := ""

	if len(args) > 0 {
		if s, ok := args[0].(string); ok {
			msg = s
		}
	}

	m.record("fatal", msg)
}

func (m *mockLoggerFieldsRecorder) Fatalf(format string, args ...any) {
	m.record("fatal", fmt.Sprintf(format, args...))
}

func (m *mockLoggerFieldsRecorder) Fatalln(args ...any) {
	m.record("fatal", fmt.Sprint(args...))
}

func (m *mockLoggerFieldsRecorder) WithDefaultMessageTemplate(s string) libLog.Logger { return m }
func (m *mockLoggerFieldsRecorder) Sync() error                                       { return nil }

func (m *mockLoggerFieldsRecorder) WithFields(fields ...any) libLog.Logger {
	return &mockLoggerFieldsRecorder{parent: m.parent, fields: fields}
}

// FieldsToMap converts a slice of key-value pairs to a map for easier assertions.
func FieldsToMap(fields []any) map[string]any {
	result := make(map[string]any)

	for i := 0; i < len(fields)-1; i += 2 {
		if key, ok := fields[i].(string); ok {
			result[key] = fields[i+1]
		}
	}

	return result
}
