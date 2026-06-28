# fast — VHS demo tape
# https://github.com/charmbracelet/vhs
#
# Generate the GIF with:
#   vhs demo.tape

Output demo.gif

Require fast

Set Shell bash
Set FontFamily "JetBrains Mono"
Set FontSize 32
Set Width 1100
Set Height 340
Set Padding 40
Set TypingSpeed 120ms

# Clean, minimal prompt
Hide
Type "export PS1='> '"
Enter
Type "clear"
Enter
Show

Sleep 750ms
Type "fast"
Sleep 750ms
Enter

# Let the 10s speed test run and settle on its final reading
Sleep 10s
