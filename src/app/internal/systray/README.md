Copia ridotta di [fyne.io/systray](https://github.com/fyne-io/systray)
(commit 5545d85, licenza Apache-2.0, vedi LICENSE): solo la parte per Linux
(StatusNotifierItem e DBusMenu via D-Bus, senza cgo). Usata dall'icona nel
vassoio di MIMUS per Linux. Unica modifica: i percorsi di import e il vincolo
`//go:build linux`.
