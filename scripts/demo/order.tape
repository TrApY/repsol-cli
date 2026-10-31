# The README demo. Run it with `make demo`, which starts the fake backend and
# sets DEMO_ENV; every value on screen is synthetic.

Output images/demos/order.gif

Set Shell "bash"
Set FontSize 18
Set Width 1200
Set Height 540
Set Padding 24
Set TypingSpeed 45ms
Set Theme { "name": "repsol-cli", "black": "#0d1117", "red": "#ff7b72", "green": "#7ee787", "yellow": "#e3b341", "blue": "#79c0ff", "magenta": "#d2a8ff", "cyan": "#a5d6ff", "white": "#c9d1d9", "brightBlack": "#6e7681", "brightRed": "#ffa198", "brightGreen": "#56d364", "brightYellow": "#e3b341", "brightBlue": "#79c0ff", "brightMagenta": "#d2a8ff", "brightCyan": "#a5d6ff", "brightWhite": "#f2f2f2", "background": "#0d1117", "foreground": "#f2f2f2", "selection": "#30363d", "cursor": "#f2f2f2" }

Hide
Type `. "$DEMO_ENV" && repsol address set 'Calle Falsa 1, Pueblo' --floor 2 --door B >/dev/null && clear`
Enter
Sleep 500ms
Show

Type "repsol catalog list | jq -c '.[] | {material_id, name, price}'"
Sleep 400ms
Enter
Sleep 2.5s

Type "repsol order create --dry-run"
Sleep 400ms
Enter
Sleep 3.5s

Hide
Type "clear"
Enter
Show

Type "repsol order create"
Sleep 400ms
Enter
Sleep 3.5s

Type "repsol order create   # a second bottle is refused while one is pending"
Sleep 400ms
Enter
Sleep 4.5s
