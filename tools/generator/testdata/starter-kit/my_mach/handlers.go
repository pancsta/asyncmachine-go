package mymach

import am "github.com/pancsta/asyncmachine-go/pkg/machine"

var _ = ss.Wet

// state-state negotiation handlers

func (h *Handlers) WetWater(e *am.Event) bool { return true }
func (h *Handlers) WetDry(e *am.Event) bool { return true }

// globals

func (h *Handlers) WetAny(e *am.Event) bool { return true }
func (h *Handlers) AnyWet(e *am.Event) bool { return true }

var _ = ss.Water

// state-state negotiation handlers

func (h *Handlers) WaterWet(e *am.Event) bool { return true }
func (h *Handlers) WaterDry(e *am.Event) bool { return true }

// globals

func (h *Handlers) WaterAny(e *am.Event) bool { return true }
func (h *Handlers) AnyWater(e *am.Event) bool { return true }

var _ = ss.Dry

// state-state negotiation handlers

func (h *Handlers) DryWet(e *am.Event) bool { return true }
func (h *Handlers) DryWater(e *am.Event) bool { return true }

// globals

func (h *Handlers) DryAny(e *am.Event) bool { return true }
func (h *Handlers) AnyDry(e *am.Event) bool { return true }
