// Copyright (c) 2026 Lerian Studio. All rights reserved.
// Use of this source code is governed by the Elastic License 2.0
// that can be found in the LICENSE file.

//go:build e2e

package steps

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/cucumber/godog"

	"tracer/internal/testutil"
	"tracer/tests/end2end/support"
)

func registerRuleSteps(ctx *godog.ScenarioContext, sc *support.ScenarioContext) {
	// --- Rule creation (direct style) ---

	ctx.Step(`^(\w+) creates a rule called "([^"]*)"$`, func(persona, name string) error {
		// Store name for subsequent "configures" steps.
		sc.InitPendingRule(name, "")
		return nil
	})

	ctx.Step(`^(?:he|she) configures it to deny PIX transactions above R\$([0-9,.]+) from accounts less than (\d+) days old$`,
		func(amount string, days int) error {
			if sc.PendingRule == nil {
				return fmt.Errorf("no pending rule — call 'creates a rule' first")
			}

			amt := normalizeAmount(amount)
			sc.PendingRule.Action = "DENY"
			sc.PendingRule.Expression = fmt.Sprintf(
				`transactionType == "PIX" && amount > %s && "accountAgeDays" in metadata && metadata["accountAgeDays"] < %d`,
				amt, days,
			)
			sc.PendingRule.Scopes = []testutil.ScopeInput{{TransactionType: testutil.Ptr("PIX")}}

			return nil
		})

	ctx.Step(`^(?:he|she) configures it to deny PIX above R\$([0-9,.]+) from accounts less than (\d+) days old, excluding VIP tier customers$`,
		func(amount string, days int) error {
			if sc.PendingRule == nil {
				return fmt.Errorf("no pending rule — call 'creates a rule' first")
			}

			amt := normalizeAmount(amount)
			sc.PendingRule.Action = "DENY"
			sc.PendingRule.Expression = fmt.Sprintf(
				`transactionType == "PIX" && amount > %s && "accountAgeDays" in metadata && metadata["accountAgeDays"] < %d && metadata["customerTier"] != "VIP"`,
				amt, days,
			)
			sc.PendingRule.Scopes = []testutil.ScopeInput{{TransactionType: testutil.Ptr("PIX")}}

			return nil
		})

	ctx.Step(`^(?:he|she) configures it to flag for review international transactions between R\$([0-9,.]+) and R\$([0-9,.]+) from accounts less than (\d+) days old$`,
		func(minAmt, maxAmt string, days int) error {
			if sc.PendingRule == nil {
				return fmt.Errorf("no pending rule — call 'creates a rule' first")
			}

			min := normalizeAmount(minAmt)
			max := normalizeAmount(maxAmt)
			sc.PendingRule.Action = "REVIEW"
			sc.PendingRule.Expression = fmt.Sprintf(
				`size(merchant) > 0 && merchant["country"] != "BR" && amount > %s && amount <= %s && "accountAgeDays" in metadata && metadata["accountAgeDays"] < %d`,
				min, max, days,
			)

			return nil
		})

	ctx.Step(`^(?:he|she) configures it to explicitly allow transactions below R\$([0-9,.]+) from "([^"]*)" and "([^"]*)"$`,
		func(amount, merchant1, merchant2 string) error {
			if sc.PendingRule == nil {
				return fmt.Errorf("no pending rule — call 'creates a rule' first")
			}

			amt := normalizeAmount(amount)
			uuid1 := sc.MerchantUUIDs[merchant1]
			uuid2 := sc.MerchantUUIDs[merchant2]

			if uuid1 == "" || uuid2 == "" {
				return fmt.Errorf("merchant UUIDs not resolved for %q and/or %q — ensure Background step ran", merchant1, merchant2)
			}

			sc.PendingRule.Action = "ALLOW"
			sc.PendingRule.Expression = fmt.Sprintf(
				`size(merchant) > 0 && (merchant["merchantId"] == "%s" || merchant["merchantId"] == "%s") && amount < %s`,
				uuid1, uuid2, amt,
			)

			return nil
		})

	// Flush pending rule → POST /v1/rules
	ctx.Step(`^the rule should be created successfully in Draft status$`, func() error {
		if sc.PendingRule != nil && sc.PendingRule.Action != "" {
			// Flush the pending rule
			rule, status, err := support.CreateRuleE(
				sc.PendingRule.Name,
				sc.PendingRule.Expression,
				sc.PendingRule.Action,
				sc.PendingRule.Scopes,
			)
			if err != nil {
				return fmt.Errorf("creating rule: %w", err)
			}

			sc.LastRule = rule
			sc.LastRuleHTTP = status
			sc.PendingRule = nil

			if status != http.StatusCreated {
				return fmt.Errorf("expected 201 Created, got %d", status)
			}

			sc.RegisterRule(rule.Name, rule.ID)
		}

		if sc.LastRule.ID == "" {
			return fmt.Errorf("no rule available: neither created from pending nor previously set")
		}

		if sc.LastRule.Status != "DRAFT" {
			return fmt.Errorf("expected DRAFT status, got %q", sc.LastRule.Status)
		}

		return nil
	})

	ctx.Step(`^the rule should have a unique identifier$`, func() error {
		if sc.LastRule.ID == "" {
			return fmt.Errorf("rule ID is empty")
		}

		return nil
	})

	ctx.Step(`^the rule action should be (\w+)$`, func(action string) error {
		expected := strings.ToUpper(action)
		if sc.LastRule.Action != expected {
			return fmt.Errorf("expected action %q, got %q", expected, sc.LastRule.Action)
		}

		return nil
	})

	// --- Rule status management ---

	ctx.Step(`^the rule "([^"]*)" is in Draft status$`, func(name string) error {
		ruleID := sc.FindRuleID(name)
		if ruleID == "" {
			return fmt.Errorf("rule %q not found in scenario context or via API", name)
		}

		rule, status, err := support.GetRuleE(ruleID)
		if err != nil {
			return fmt.Errorf("getting rule: %w", err)
		}

		if status != http.StatusOK {
			return fmt.Errorf("expected 200, got %d", status)
		}

		if rule.Status != "DRAFT" {
			return fmt.Errorf("expected DRAFT, got %q", rule.Status)
		}

		sc.LastRule = rule

		return nil
	})

	ctx.Step(`^the rule "([^"]*)" is Active$`, func(name string) error {
		ruleID := sc.FindRuleID(name)
		if ruleID == "" {
			return fmt.Errorf("rule %q not found in scenario context", name)
		}

		rule, status, err := support.GetRuleE(ruleID)
		if err != nil {
			return fmt.Errorf("getting rule: %w", err)
		}

		if status != http.StatusOK {
			return fmt.Errorf("expected 200, got %d", status)
		}

		if rule.Status != "ACTIVE" {
			return fmt.Errorf("expected ACTIVE, got %q", rule.Status)
		}

		sc.LastRule = rule

		return nil
	})

	ctx.Step(`^(\w+) activates the rule$`, func(persona string) error {
		if sc.LastRule.ID == "" {
			return fmt.Errorf("no rule in context to activate")
		}

		rule, status, err := support.ActivateRuleE(sc.LastRule.ID)
		if err != nil {
			return fmt.Errorf("activating rule: %w", err)
		}

		sc.LastRule = rule
		sc.LastRuleHTTP = status

		if status != http.StatusOK {
			return fmt.Errorf("expected 200, got %d", status)
		}

		return nil
	})

	ctx.Step(`^(\w+) activates the rule "([^"]*)"$`, func(persona, name string) error {
		ruleID := sc.FindRuleID(name)
		if ruleID == "" {
			return fmt.Errorf("rule %q not found", name)
		}

		rule, status, err := support.ActivateRuleE(ruleID)
		if err != nil {
			return fmt.Errorf("activating rule: %w", err)
		}

		sc.LastRule = rule
		sc.LastRuleHTTP = status

		if status != http.StatusOK {
			return fmt.Errorf("expected 200, got %d", status)
		}

		return nil
	})

	ctx.Step(`^(\w+) deactivates the rule$`, func(persona string) error {
		if sc.LastRule.ID == "" {
			return fmt.Errorf("no rule in context to deactivate")
		}

		rule, status, err := support.DeactivateRuleE(sc.LastRule.ID)
		if err != nil {
			return fmt.Errorf("deactivating rule: %w", err)
		}

		sc.LastRule = rule
		sc.LastRuleHTTP = status

		if status != http.StatusOK {
			return fmt.Errorf("expected 200 deactivating rule, got %d", status)
		}

		return nil
	})

	ctx.Step(`^(\w+) deactivates the "([^"]*)" rule$`, func(persona, name string) error {
		ruleID := sc.FindRuleID(name)
		if ruleID == "" {
			return fmt.Errorf("rule %q not found", name)
		}

		rule, status, err := support.DeactivateRuleE(ruleID)
		if err != nil {
			return fmt.Errorf("deactivating rule: %w", err)
		}

		sc.LastRule = rule
		sc.LastRuleHTTP = status

		if status != http.StatusOK {
			return fmt.Errorf("expected 200 deactivating rule %q, got %d", name, status)
		}

		return nil
	})

	ctx.Step(`^the rule should become Active$`, func() error {
		if sc.LastRule.Status != "ACTIVE" {
			return fmt.Errorf("expected ACTIVE, got %q", sc.LastRule.Status)
		}

		return nil
	})

	ctx.Step(`^the rule should become Inactive$`, func() error {
		if sc.LastRule.Status != "INACTIVE" {
			return fmt.Errorf("expected INACTIVE, got %q", sc.LastRule.Status)
		}

		return nil
	})

	ctx.Step(`^the activation time should be recorded$`, func() error {
		if sc.LastRule.ActivatedAt == nil || *sc.LastRule.ActivatedAt == "" {
			return fmt.Errorf("activatedAt should be set")
		}

		return nil
	})

	ctx.Step(`^the deactivation time should be recorded$`, func() error {
		if sc.LastRule.DeactivatedAt == nil || *sc.LastRule.DeactivatedAt == "" {
			return fmt.Errorf("deactivatedAt should be set")
		}

		return nil
	})

	// --- Builder pattern (J3) ---

	ctx.Step(`^an? (\w+) rule called "([^"]*)"$`, func(action, name string) error {
		sc.InitPendingRule(name, strings.ToUpper(action))
		return nil
	})

	ctx.Step(`^it targets wire transfers above R\$([0-9,.]+)$`, func(amount string) error {
		if sc.PendingRule == nil {
			return fmt.Errorf("no pending rule")
		}

		amt := normalizeAmount(amount)
		sc.PendingRule.Expression = fmt.Sprintf(`transactionType == "WIRE" && amount > %s`, amt)

		return nil
	})

	ctx.Step(`^it targets transactions from merchant "([^"]*)"$`, func(name string) error {
		if sc.PendingRule == nil {
			return fmt.Errorf("no pending rule")
		}

		sc.PendingRule.Expression = fmt.Sprintf(`size(merchant) > 0 && merchant["name"] == "%s"`, name)

		return nil
	})

	ctx.Step(`^the rule is created$`, func() error {
		if sc.PendingRule == nil {
			return fmt.Errorf("no pending rule to create")
		}

		rule, status, err := support.CreateRuleE(
			sc.PendingRule.Name,
			sc.PendingRule.Expression,
			sc.PendingRule.Action,
			sc.PendingRule.Scopes,
		)
		if err != nil {
			return fmt.Errorf("creating rule: %w", err)
		}

		sc.LastRule = rule
		sc.LastRuleHTTP = status
		sc.PendingRule = nil

		if status != http.StatusCreated {
			return fmt.Errorf("expected 201, got %d", status)
		}

		sc.RegisterRule(rule.Name, rule.ID)

		return nil
	})

	ctx.Step(`^the rule is activated$`, func() error {
		if sc.LastRule.ID == "" {
			return fmt.Errorf("no rule in context to activate")
		}

		rule, status, err := support.ActivateRuleE(sc.LastRule.ID)
		if err != nil {
			return fmt.Errorf("activating rule: %w", err)
		}

		sc.LastRule = rule
		sc.LastRuleHTTP = status

		if status != http.StatusOK {
			return fmt.Errorf("expected 200, got %d", status)
		}

		return nil
	})

	ctx.Step(`^the rule should be Active$`, func() error {
		if sc.LastRule.Status != "ACTIVE" {
			return fmt.Errorf("expected ACTIVE, got %q", sc.LastRule.Status)
		}

		return nil
	})

	// --- J4 direct create + batch activate ---

	ctx.Step(`^(\w+) creates an? (\w+) rule called "([^"]*)" for all crypto transactions$`,
		func(persona, action, name string) error {
			rule, status, err := support.CreateRuleE(
				name,
				`transactionType == "CRYPTO"`,
				strings.ToUpper(action),
				nil,
			)
			if err != nil {
				return fmt.Errorf("creating rule: %w", err)
			}

			if status != http.StatusCreated {
				return fmt.Errorf("expected 201, got %d", status)
			}

			sc.RegisterRule(rule.Name, rule.ID)
			sc.LastRule = rule

			return nil
		})

	ctx.Step(`^all three rules are activated$`, func() error {
		names := make([]string, 0, len(sc.Rules))
		for name := range sc.Rules {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			id := sc.Rules[name]
			rule, status, err := support.ActivateRuleE(id)
			if err != nil {
				return fmt.Errorf("activating rule %q: %w", name, err)
			}

			if status != http.StatusOK {
				return fmt.Errorf("activating rule %q: expected 200, got %d", name, status)
			}

			sc.LastRule = rule
		}

		return nil
	})

	// --- J7 shortcut steps (create + activate in one step) ---

	ctx.Step(`^a deny rule called "([^"]*)" is active, blocking crypto transactions above R\$([0-9,.]+)$`,
		func(name, amount string) error {
			amt := normalizeAmount(amount)
			rule, status, err := support.CreateRuleE(
				name,
				fmt.Sprintf(`transactionType == "CRYPTO" && amount > %s`, amt),
				"DENY",
				nil,
			)
			if err != nil {
				return fmt.Errorf("creating rule: %w", err)
			}

			if status != http.StatusCreated {
				return fmt.Errorf("expected 201, got %d", status)
			}

			sc.RegisterRule(rule.Name, rule.ID)

			// Activate immediately
			rule, status, err = support.ActivateRuleE(rule.ID)
			if err != nil {
				return fmt.Errorf("activating rule: %w", err)
			}

			if status != http.StatusOK {
				return fmt.Errorf("expected 200, got %d", status)
			}

			sc.LastRule = rule

			return nil
		})

	ctx.Step(`^a review rule called "([^"]*)" is active, flagging wire transfers above R\$([0-9,.]+)$`,
		func(name, amount string) error {
			amt := normalizeAmount(amount)
			rule, status, err := support.CreateRuleE(
				name,
				fmt.Sprintf(`transactionType == "WIRE" && amount > %s`, amt),
				"REVIEW",
				nil,
			)
			if err != nil {
				return fmt.Errorf("creating rule: %w", err)
			}

			if status != http.StatusCreated {
				return fmt.Errorf("expected 201, got %d", status)
			}

			sc.RegisterRule(rule.Name, rule.ID)

			// Activate immediately
			rule, status, err = support.ActivateRuleE(rule.ID)
			if err != nil {
				return fmt.Errorf("activating rule: %w", err)
			}

			if status != http.StatusOK {
				return fmt.Errorf("expected 200, got %d", status)
			}

			sc.LastRule = rule

			return nil
		})

	ctx.Step(`^all three rules should be Active$`, func() error {
		for name, id := range sc.Rules {
			rule, status, err := support.GetRuleE(id)
			if err != nil {
				return fmt.Errorf("getting rule %q: %w", name, err)
			}

			if status != http.StatusOK {
				return fmt.Errorf("getting rule %q: expected 200, got %d", name, status)
			}

			if rule.Status != "ACTIVE" {
				return fmt.Errorf("rule %q: expected ACTIVE, got %q", name, rule.Status)
			}
		}

		return nil
	})
}

// normalizeAmount removes comma thousand-separators from amount strings.
// Amounts use US/Brazilian format: comma for thousands, dot for decimals.
// "20,000" → "20000", "15,000.00" → "15000.00"
func normalizeAmount(s string) string {
	// Remove commas (thousand separators)
	return strings.ReplaceAll(s, ",", "")
}
