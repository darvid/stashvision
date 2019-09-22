#NoEnv
#Persistent
#SingleInstance, Force
DetectHiddenWindows, On
GroupAdd, PoEexe, ahk_exe PathOfExile.exe
GroupAdd, PoEexe, ahk_exe PathOfExileSteam.exe
GroupAdd, PoEexe, ahk_exe PathOfExile_x64.exe
GroupAdd, PoEexe, ahk_exe PathOfExile_x64Steam.exe
; #IfWinActive Path of Exile ahk_class POEWindowClass ahk_group PoEexe
SendMode Input
SetWorkingDir %A_ScriptDir%
#Include Gdip_All.ahk
DllCall("AllocConsole")
FileAppend stashvision initializing, CONOUT$
Sleep 1
WinHide % "ahk_id " DllCall("GetConsoleWindow", "ptr")

global configFile := "stashvision.ini"
global indexServerPid := 0
global rewardSets := []
global currentSetIndex := 0
global hwndOverlay := 0
global hwndSearch := 0
global searchString := ""
global searchHidden := true

If !gdiToken := Gdip_Startup() {
    MsgBox, 48, gdiplus error, Gdiplus failed to start. Please ensure you have gdiplus on your system
    ExitApp, 1, 1
}
OnExit, Exit

ShellExec(command, ByRef stdout) {
    shell := ComObjCreate("WScript.Shell")
    exec := shell.Exec(command)
    stdout := ""
    while, !exec.StdOut.AtEndOfStream
        stdout := exec.StdOut.ReadAll()
}

ParsePositions(output, sets) {
    setsOutput := StrSplit(output, "---")
    for setIndex, setLines in setsOutput {
        set := []
        lines := StrSplit(setLines, "`n")
        for index, line in lines {
            parts := StrSplit(line, ":")
            dimensions := StrSplit(parts[1], "x")
            position := StrSplit(parts[2], ",")
            if dimensions.Length() != 2 || position.Length() != 2 {
                Continue
            }
            objPosition := {"width": dimensions[1]
                            ,"height": dimensions[2]
                            ,"x": position[1]
                            ,"y": position[2]}
            set.Push(objPosition)
        }
        if (set.Length() > 0) {
            sets.Push(set)
        }
    }
}

RunIndexServer(ByRef pid) {
    IniRead, poeSessionId, %configFile%, General, SessionId
    IniRead, accountName, %configFile%, General, AccountName
    IniRead, tabIndex, %configFile%, Stash, DumpTabIndex, 0
    if (poeSessionId == "ERROR") || (accountName == "ERROR") {
        MsgBox, 48, stashvision error, SessionId or AccountName missing from config file
        ExitApp, 1, 1
    }
    Run stashvision-go\stashvision.exe server -s=%poeSessionId% -a=%accountName% -v -t=%tabIndex% -l=server.log,, hide, pid
}

CreateSearchWindow(width, height, ByRef hwnd, ByRef searchString, hidden) {
    w := width - 20
    Gui, Search:New, -Caption +LastFound +AlwaysOnTop +ToolWindow +OwnDialogs -SysMenu, Stashvision
    Gui, Search:Add, Edit, r1 vsearchString w%w% -WantReturn,
    Gui, Search:Add, Button, Default, OK
    if !hidden {
        Gui, Show, w%width% h%height%
        searchHidden := false
    }
    hwnd := WinExist()
}

CreateOverlay(width, height, ByRef hwnd, ByRef graphics, ByRef hbm, ByRef hdc, ByRef obm) {
    Gui, 1: -Caption E0x80000 +LastFound +AlwaysOnTop +ToolWindow +OwnDialogs
    Gui, 1: Show, NA
    hwnd := WinExist()
    hbm := CreateDIBSection(width, height)
    hdc := CreateCompatibleDC()
    obm := SelectObject(hdc, hbm)
    graphics := Gdip_GraphicsFromHDC(hdc)
    Gdip_SetSmoothingMode(graphics, 4)
}

CloseSearchWindow() {
    searchHidden := true
    Gui, Search:Submit
    hwndSearch := 0
}

CloseOverlay() {
    ; global hwndOverlay
    WinClose, ahk_id %hwndOverlay%
    hwndOverlay := 0
}

DeleteGraphics(hbm, hdc, obm, graphics) {
    SelectObject(hdc, obm)
    DeleteObject(hbm)
    DeleteDC(hdc)
    Gdip_DeleteGraphics(graphics)
}

GetStashPosition(ByRef stashX, ByRef stashY, ByRef stashWidth, ByRef stashHeight) {
    WinGetPos, poeX, poeY, poeWidth, poeHeight, Path of Exile
    stashX := 23
    stashY := Round(poeHeight * 0.15)
    stashWidth := Round(poeWidth * 0.33)
    stashHeight := poeHeight - stashY - 380
}

HighlightChaosRecipe() {
    if (!WinActive("ahk_group PoEexe")) {
        return
    }
    global hwndOverlay, rewardSets, currentSetIndex, hbm, hdc, obm, graphics
    GetStashPosition(stashX, stashY, stashWidth, stashHeight)
    if (hwndOverlay == 0) {
        CreateOverlay(stashWidth, stashHeight, hwnd, graphics, hbm, hdc, obm)
        hwndOverlay := hwnd
    }
    if (rewardSets.Length() > 0) && (currentSetIndex != rewardSets.Length()) {
        currentSetIndex++
        positions := rewardSets[currentSetIndex]
    } else {
        rewardSets := []
        IniRead, tabIndex, %configFile%, Stash, DumpTabIndex, 0
        cmd := "stashvision-go\stashvision.exe recipe -n=unid_chaos -p -t=" tabIndex
        ShellExec(cmd, output)
        ParsePositions(output, rewardSets)
        currentSetIndex = 1
        positions := rewardSets[currentSetIndex]
    }
    if (positions.Length() == 0) {
        DeleteGraphics(hbm, hdc, obm, graphics)
        CloseOverlay()
    } else {
        Gdip_GraphicsClear(graphics)
        IniRead, borderColor, %configFile%, Display, DefaultHighlightBorder, 0xffffffff
        IniRead, tabQuad, %configFile%, Stash, DumpTabQuad, false
        for index, p in positions {
            HighlightItem(stashX, stashY, stashWidth, stashHeight, hwndOverlay, graphics
                         , hdc, p.width, p.height, p.x, p.y, tabQuad == "true", borderColor)
        }
        ; DeleteGraphics(hbm, hdc, obm, graphics)
    }
}

HighlightItem(stashX, stashY, stashWidth, stashHeight, hwnd, graphics, hdc, width, height, x, y, isQuadTab, borderColor) {
    if isQuadTab {
        rectWidth := 35
        rectHeight := 35
    } else {
        rectWidth := 70
        rectHeight := 70
    }
    pen := Gdip_CreatePen(borderColor, 2)
    Gdip_DrawRectangle(graphics, pen, rectWidth * x, rectHeight * y, rectWidth * width, rectHeight * height)
    Gdip_DeletePen(pen)

    UpdateLayeredWindow(hwnd, hdc, stashX, stashY, stashWidth, stashHeight)
}

PerformSearch() {
    CloseSearchWindow()
    WinWaitActive, ahk_group PoEexe,, 1
    if ErrorLevel {
        return
    }
    cmd := "stashvision-go\stashvision.exe query -s=""" searchString """ -p"
    ShellExec(cmd, output)
    global hwndOverlay, rewardSets, currentSetIndex, hbm, hdc, obm, graphics
    GetStashPosition(stashX, stashY, stashWidth, stashHeight)
    if (hwndOverlay == 0) {
        CreateOverlay(stashWidth, stashHeight, hwnd, graphics, hbm, hdc, obm)
        hwndOverlay := hwnd
    }
    sets := []
    ParsePositions(output, sets)
    IniRead, borderColor, %configFile%, Display, DefaultHighlightBorder, 0xffffffff
    IniRead, tabQuad, %configFile%, Stash, DumpTabQuad, false
    if (sets.Length() == 0) {
        DeleteGraphics(hbm, hdc, obm, graphics)
        CloseOverlay()
    } else {
        positions := sets[1]
        for index, p in positions {
            HighlightItem(stashX, stashY, stashWidth, stashHeight, hwndOverlay, graphics
                         , hdc, p.width, p.height, p.x, p.y, tabQuad == "true", borderColor)
        }
    }
}

Screenshot(hwnd, outputFile) {
    If !token := Gdip_Startup() {
        MsgBox, 48, gdiplus error!, Gdiplus failed to start. Please ensure you have gdiplus on your system
        ExitApp, 1
    }
    bitmap := Gdip_BitmapFromHWND(hwnd)
    Gdip_SaveBitmapToFile(bitmap, outputFile, 100)
    Gdip_DisposeImage(bitmap)
    Gdip_Shutdown(token)
}

RegisterHotkeys() {
    IniRead, chaosHotkey, configFile, Bindings, ChaosRecipe, +!c
    IniRead, searchHotkey, configFile, Bindings, Search, +!s
    Hotkey, %chaosHotkey%, HighlightChaosRecipe
    Hotkey, %searchHotkey%, ShowSearch
}

RegisterHotkeys()
RunIndexServer(indexServerPid)
SetTimer, CheckIndexServer, 250
CreateSearchWindow(200, 30, hwndSearch, searchString, true)
return

CheckIndexServer:
Process, Exist, %indexServerPid%
if !ErrorLevel {
    ExitApp, 1
} else {
    SetTimer, CheckIndexServer, 250
}
return

SearchButtonOK:
PerformSearch()
return

ShowSearch:
searchHidden := false
Gui, Search:Show, w200 h30
return

Exit:
CloseOverlay()
Gdip_Shutdown(gdiToken)
; XXX: /f shouldn't be required...
Run taskkill.exe /f /pid %indexServerPid%
ExitApp
return

$Escape::
if (!searchHidden) && (WinActive("ahk_id " hwndSearch)) {
    Gui, Search:Hide
} else if (hwndOverlay == 0) || (WinActive("ahk_group PoEexe") == 0) {
    Send, {Escape}
} else {
    CloseOverlay()
}
return
