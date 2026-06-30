# fast — VHS demo tape
# https://github.com/charmbracelet/vhs
#
# Generate the GIF with:
#   vhs demo.tape

Output demo.gif

Require fast

Set Shell bash
Set FontFamily "JetBrains Mono"
Set FontSize 96
Set Width 3300
Set Height 880
Set Padding 120
Set TypingSpeed 100ms

# Clean, minimal prompt
Hide
Type "export PS1='\[\e[38;2;99;102;241m\]> \[\e[0m\]'"
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
