Output docs/demo.gif

Require jq

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font Mono"
Set FontSize 18
Set Width 800
Set Height 457
Set Padding 16
Set Framerate 10
Set TypingSpeed 30ms
Set Theme {"background":"#11111b","foreground":"#cdd6f4","black":"#45475a","brightBlack":"#585b70","red":"#f38ba8","brightRed":"#f38ba8","green":"#a6e3a1","brightGreen":"#a6e3a1","yellow":"#f9e2af","brightYellow":"#f9e2af","blue":"#89b4fa","brightBlue":"#89b4fa","magenta":"#cba6f7","brightMagenta":"#cba6f7","cyan":"#94e2d5","brightCyan":"#94e2d5","white":"#bac2de","brightWhite":"#a6adc8"}

Hide
Type "export PATH=$PWD/bin:$PATH"
Enter
Type "clear"
Enter
Show

Type "ecom --provider wallapop search 'gravel bike' --filter latitude=41.3874 --filter longitude=2.1686 --filter max_distance_km=50 | jq '[.data.items[:3][] | {name, price: .price.display, city: .provider_data.wallapop.city, distance_km: .provider_data.wallapop.distance_km}]'"
Sleep 500ms
Enter
Sleep 5s
