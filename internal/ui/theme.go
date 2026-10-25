package ui

import "github.com/charmbracelet/lipgloss"

// Palette: green is the primary role, brown is the accent and border role.
// Each role replaces one of the reference's ANSI roles:
//
//	reference cyan (prompt, spinner, input border)  -> colorGreen
//	reference bright cyan labels                   -> colorGreenLight
//	reference yellow (confirmation)                -> colorBrown / colorTan
//	reference grey 240 (dividers, borders)         -> colorDarkBrown
//	reference grey 245 (timestamps)                -> colorTan
//	reference red (errors)                         -> colorError
//	reference default foreground                   -> terminal default
const (
	colorGreen      = "#22C55E"
	colorGreenLight = "#86EFAC"
	colorGreenDeep  = "#166534"
	colorBrown      = "#8B5A2B"
	colorTan        = "#C8A27A"
	colorDarkBrown  = "#5C3D1E"
	colorError      = "#EF4444"
)

var (
	stylePrompt      = lipgloss.NewStyle().Foreground(lipgloss.Color(colorGreen)).Bold(true)
	stylePromptText  = lipgloss.NewStyle().Bold(true)
	styleAccentBold  = lipgloss.NewStyle().Foreground(lipgloss.Color(colorGreen)).Bold(true)
	styleDim         = lipgloss.NewStyle().Faint(true)
	styleTool        = lipgloss.NewStyle().Foreground(lipgloss.Color(colorTan))
	styleError       = lipgloss.NewStyle().Foreground(lipgloss.Color(colorError))
	styleDivider     = lipgloss.NewStyle().Foreground(lipgloss.Color(colorDarkBrown))
	styleTimestamp   = lipgloss.NewStyle().Foreground(lipgloss.Color(colorTan)).Bold(true)
	styleConfirmMark = lipgloss.NewStyle().Foreground(lipgloss.Color(colorTan)).Bold(true)
	styleBanner      = lipgloss.NewStyle().Foreground(lipgloss.Color(colorGreen)).Bold(true)
	styleShadow      = lipgloss.NewStyle().Foreground(lipgloss.Color(colorBrown)).Bold(true)
	styleMascotFace  = lipgloss.NewStyle().Foreground(lipgloss.Color(colorTan)).Bold(true)
	styleSubtitle    = lipgloss.NewStyle().Foreground(lipgloss.Color(colorGreenLight))
	styleStatus      = lipgloss.NewStyle().Foreground(lipgloss.Color(colorGreenDeep))
	styleModalBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	// styleAgentBadge names the active agent above the input: dark text on the
	// primary green, so it stands out from the dim status text around it.
	styleAgentBadge = lipgloss.NewStyle().Background(lipgloss.Color(colorGreen)).Foreground(lipgloss.Color(colorBadgeText)).Bold(true).Padding(0, 1)
)

// colorBadgeText is the near-black text of the agent badge.
const colorBadgeText = "#052E16"
