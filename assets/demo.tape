# VHS tape for the README demo. Render with: make demo
# Requires vhs (https://github.com/charmbracelet/vhs) and a built groot binary on PATH.
# GROOT_DEMO_HOME points at a throwaway home so the recording never shows real data.
# The working directory must hold an Anthropic key in .env; no model call is made.

Output assets/demo.gif

Set Shell "zsh"
Set FontSize 15
Set Width 1200
Set Height 720
Set Padding 16
Set Theme "Catppuccin Mocha"
Set TypingSpeed 60ms

Hide
Type `export HOME="${GROOT_DEMO_HOME:?set GROOT_DEMO_HOME}" GROOT_HOME="${GROOT_DEMO_HOME}/.groot" && clear`
Enter
Show

Type "groot"
Sleep 500ms
Enter
Sleep 4s

# The first start asks how agents should call you.
Type "Tincho"
Sleep 500ms
Enter
Sleep 2s

Type `/agent new reviewer "reviews diffs before merge"`
Sleep 800ms
Escape
Sleep 500ms
Enter
Sleep 3s

Type "/space assign reviewer"
Sleep 800ms
Escape
Sleep 500ms
Enter
Sleep 3s

Type "/agent list"
Sleep 800ms
Escape
Sleep 500ms
Enter
Sleep 4s

Hide
Ctrl+C
Sleep 1s
