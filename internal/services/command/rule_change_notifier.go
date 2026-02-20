// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

package command

// RuleChangeNotifier is implemented by components that need to be notified
// when a rule change is persisted (activate, deactivate, delete, draft).
// The sync worker implements this to trigger an immediate cache refresh
// instead of waiting for the next poll interval.
type RuleChangeNotifier interface {
	Notify()
}
