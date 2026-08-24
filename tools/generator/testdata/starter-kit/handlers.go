package starter_kit

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

var _ = ss.Start

// state-state negotiation handlers

func (h *Handlers) StartBaseDBReady(e *am.Event) bool { return true }
func (h *Handlers) StartBaseDBSaving(e *am.Event) bool { return true }
func (h *Handlers) StartBaseDBStarting(e *am.Event) bool { return true }
func (h *Handlers) StartCharacterReady(e *am.Event) bool { return true }
func (h *Handlers) StartCheckStories(e *am.Event) bool { return true }
func (h *Handlers) StartRestoreCharacter(e *am.Event) bool { return true }
func (h *Handlers) StartGenCharacter(e *am.Event) bool { return true }

// globals

func (h *Handlers) StartAny(e *am.Event) bool { return true }
func (h *Handlers) AnyStart(e *am.Event) bool { return true }

var _ = ss.BaseDBReady

// state-state negotiation handlers

func (h *Handlers) BaseDBReadyStart(e *am.Event) bool { return true }
func (h *Handlers) BaseDBReadyBaseDBSaving(e *am.Event) bool { return true }
func (h *Handlers) BaseDBReadyBaseDBStarting(e *am.Event) bool { return true }
func (h *Handlers) BaseDBReadyCharacterReady(e *am.Event) bool { return true }
func (h *Handlers) BaseDBReadyCheckStories(e *am.Event) bool { return true }
func (h *Handlers) BaseDBReadyRestoreCharacter(e *am.Event) bool { return true }
func (h *Handlers) BaseDBReadyGenCharacter(e *am.Event) bool { return true }

// globals

func (h *Handlers) BaseDBReadyAny(e *am.Event) bool { return true }
func (h *Handlers) AnyBaseDBReady(e *am.Event) bool { return true }

var _ = ss.BaseDBSaving

// state-state negotiation handlers

func (h *Handlers) BaseDBSavingStart(e *am.Event) bool { return true }
func (h *Handlers) BaseDBSavingBaseDBReady(e *am.Event) bool { return true }
func (h *Handlers) BaseDBSavingBaseDBStarting(e *am.Event) bool { return true }
func (h *Handlers) BaseDBSavingCharacterReady(e *am.Event) bool { return true }
func (h *Handlers) BaseDBSavingCheckStories(e *am.Event) bool { return true }
func (h *Handlers) BaseDBSavingRestoreCharacter(e *am.Event) bool { return true }
func (h *Handlers) BaseDBSavingGenCharacter(e *am.Event) bool { return true }

// globals

func (h *Handlers) BaseDBSavingAny(e *am.Event) bool { return true }
func (h *Handlers) AnyBaseDBSaving(e *am.Event) bool { return true }

var _ = ss.BaseDBStarting

// state-state negotiation handlers

func (h *Handlers) BaseDBStartingStart(e *am.Event) bool { return true }
func (h *Handlers) BaseDBStartingBaseDBReady(e *am.Event) bool { return true }
func (h *Handlers) BaseDBStartingBaseDBSaving(e *am.Event) bool { return true }
func (h *Handlers) BaseDBStartingCharacterReady(e *am.Event) bool { return true }
func (h *Handlers) BaseDBStartingCheckStories(e *am.Event) bool { return true }
func (h *Handlers) BaseDBStartingRestoreCharacter(e *am.Event) bool { return true }
func (h *Handlers) BaseDBStartingGenCharacter(e *am.Event) bool { return true }

// globals

func (h *Handlers) BaseDBStartingAny(e *am.Event) bool { return true }
func (h *Handlers) AnyBaseDBStarting(e *am.Event) bool { return true }

var _ = ss.CharacterReady

// state-state negotiation handlers

func (h *Handlers) CharacterReadyStart(e *am.Event) bool { return true }
func (h *Handlers) CharacterReadyBaseDBReady(e *am.Event) bool { return true }
func (h *Handlers) CharacterReadyBaseDBSaving(e *am.Event) bool { return true }
func (h *Handlers) CharacterReadyBaseDBStarting(e *am.Event) bool { return true }
func (h *Handlers) CharacterReadyCheckStories(e *am.Event) bool { return true }
func (h *Handlers) CharacterReadyRestoreCharacter(e *am.Event) bool { return true }
func (h *Handlers) CharacterReadyGenCharacter(e *am.Event) bool { return true }

// globals

func (h *Handlers) CharacterReadyAny(e *am.Event) bool { return true }
func (h *Handlers) AnyCharacterReady(e *am.Event) bool { return true }

var _ = ss.CheckStories

// state-state negotiation handlers

func (h *Handlers) CheckStoriesStart(e *am.Event) bool { return true }
func (h *Handlers) CheckStoriesBaseDBReady(e *am.Event) bool { return true }
func (h *Handlers) CheckStoriesBaseDBSaving(e *am.Event) bool { return true }
func (h *Handlers) CheckStoriesBaseDBStarting(e *am.Event) bool { return true }
func (h *Handlers) CheckStoriesCharacterReady(e *am.Event) bool { return true }
func (h *Handlers) CheckStoriesRestoreCharacter(e *am.Event) bool { return true }
func (h *Handlers) CheckStoriesGenCharacter(e *am.Event) bool { return true }

// globals

func (h *Handlers) CheckStoriesAny(e *am.Event) bool { return true }
func (h *Handlers) AnyCheckStories(e *am.Event) bool { return true }

var _ = ss.CheckingMenuRefs

// state-state negotiation handlers

func (h *Handlers) CheckingMenuRefsStart(e *am.Event) bool { return true }
func (h *Handlers) CheckingMenuRefsBaseDBReady(e *am.Event) bool { return true }
func (h *Handlers) CheckingMenuRefsBaseDBSaving(e *am.Event) bool { return true }
func (h *Handlers) CheckingMenuRefsBaseDBStarting(e *am.Event) bool { return true }
func (h *Handlers) CheckingMenuRefsCharacterReady(e *am.Event) bool { return true }
func (h *Handlers) CheckingMenuRefsCheckStories(e *am.Event) bool { return true }
func (h *Handlers) CheckingMenuRefsRestoreCharacter(e *am.Event) bool { return true }
func (h *Handlers) CheckingMenuRefsGenCharacter(e *am.Event) bool { return true }

// globals

func (h *Handlers) CheckingMenuRefsAny(e *am.Event) bool { return true }
func (h *Handlers) AnyCheckingMenuRefs(e *am.Event) bool { return true }

var _ = ss.RestoreCharacter

// state-state negotiation handlers

func (h *Handlers) RestoreCharacterStart(e *am.Event) bool { return true }
func (h *Handlers) RestoreCharacterBaseDBReady(e *am.Event) bool { return true }
func (h *Handlers) RestoreCharacterBaseDBSaving(e *am.Event) bool { return true }
func (h *Handlers) RestoreCharacterBaseDBStarting(e *am.Event) bool { return true }
func (h *Handlers) RestoreCharacterCharacterReady(e *am.Event) bool { return true }
func (h *Handlers) RestoreCharacterCheckStories(e *am.Event) bool { return true }
func (h *Handlers) RestoreCharacterGenCharacter(e *am.Event) bool { return true }

// globals

func (h *Handlers) RestoreCharacterAny(e *am.Event) bool { return true }
func (h *Handlers) AnyRestoreCharacter(e *am.Event) bool { return true }

var _ = ss.GenCharacter

// state-state negotiation handlers

func (h *Handlers) GenCharacterStart(e *am.Event) bool { return true }
func (h *Handlers) GenCharacterBaseDBReady(e *am.Event) bool { return true }
func (h *Handlers) GenCharacterBaseDBSaving(e *am.Event) bool { return true }
func (h *Handlers) GenCharacterBaseDBStarting(e *am.Event) bool { return true }
func (h *Handlers) GenCharacterCharacterReady(e *am.Event) bool { return true }
func (h *Handlers) GenCharacterCheckStories(e *am.Event) bool { return true }
func (h *Handlers) GenCharacterRestoreCharacter(e *am.Event) bool { return true }

// globals

func (h *Handlers) GenCharacterAny(e *am.Event) bool { return true }
func (h *Handlers) AnyGenCharacter(e *am.Event) bool { return true }
