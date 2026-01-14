// Package rlm provides error recovery for the RLM orchestration system.
//
// This file provides backwards-compatible exports from the modular orchestrator package.
// New code should import github.com/rand/recurse/internal/rlm/orchestrator directly.
package rlm

import (
	"github.com/rand/recurse/internal/rlm/orchestrator"
)

// Re-export types from orchestrator package for backwards compatibility.
type (
	RecoveryConfig    = orchestrator.RecoveryConfig
	ErrorCategory     = orchestrator.ErrorCategory
	RecoveryAction    = orchestrator.RecoveryAction
	ErrorRecord       = orchestrator.ErrorRecord
	RecoveryManager   = orchestrator.RecoveryManager
	ErrorStats        = orchestrator.ErrorStats
	RecoverableError  = orchestrator.RecoverableError
)

// Re-export constants.
const (
	ErrorCategoryRetryable   = orchestrator.ErrorCategoryRetryable
	ErrorCategoryDegradable  = orchestrator.ErrorCategoryDegradable
	ErrorCategoryTerminal    = orchestrator.ErrorCategoryTerminal
	ErrorCategoryTimeout     = orchestrator.ErrorCategoryTimeout
	ErrorCategoryResource    = orchestrator.ErrorCategoryResource
)

// Re-export functions.
var (
	DefaultRecoveryConfig = orchestrator.DefaultRecoveryConfig
	NewRecoveryManager    = orchestrator.NewRecoveryManager
	WrapWithRecovery      = orchestrator.WrapWithRecovery
	IsRecoverable         = orchestrator.IsRecoverable
	ShouldDegrade         = orchestrator.ShouldDegrade
)
