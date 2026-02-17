import * as vscode from "vscode"

import execWithIndices = require("regexp-match-indices")

export interface ReplaceMatchingTextParams {
    file: string,
    match: string,
    replacement: string,
    start?: number,
    stop?: number,
    isRegex?: boolean,
    group?: number,
    count?: number
}

export async function replaceMatchingText(params: ReplaceMatchingTextParams) {
    // Display the editor window for the target file.

    const doc = await vscode.workspace.openTextDocument(params.file)
    const editor = await vscode.window.showTextDocument(doc)

    // Bail out if there was no text to match provided.

    if (!params.match)
        return

    if (params.replacement === undefined)
        return

    // Find the matching text based on whether regex or exact match.

    const lines = editor.document.lineCount

    let startLine = (params.start === undefined || params.start === null) ? 0 : params.start
    let stopLine = (params.stop === undefined || params.stop === null) ? lines : params.stop

    if (startLine < 0)
        startLine = lines + startLine

    if (startLine < 0)
        startLine = 0
    else if (startLine >= lines)
        startLine = lines - 1

    if (stopLine < 0)
        stopLine = lines + stopLine

    if (stopLine < 0)
        stopLine = 0
    else if (stopLine > lines)
        stopLine = lines

    // Determine how many matches to replace. Default is 1 (first match
    // only). A value of -1 means replace all matches.

    let maxCount = (params.count === undefined || params.count === null || params.count === 0) ? 1 : params.count
    let replaceAll = maxCount < 0

    // Collect all matches to replace.

    let matches: { startLine: number, startCol: number, endLine: number, endCol: number }[] = []

    // Check whether the match string spans multiple lines.

    let isMultiLine = params.match.indexOf('\n') >= 0

    if (isMultiLine) {
        // For multi-line matching, extract the text block within the
        // start/stop range and search within it as a single string.

        let lastLine = stopLine - 1

        if (lastLine < startLine)
            lastLine = startLine

        let rangeStart = new vscode.Position(startLine, 0)
        let rangeEnd = new vscode.Position(lastLine, editor.document.lineAt(lastLine).text.length)
        let searchRange = new vscode.Range(rangeStart, rangeEnd)
        let blockText = editor.document.getText(searchRange)
        let blockOffset = editor.document.offsetAt(rangeStart)

        if (params.isRegex) {
            let regex = new RegExp(params.match)
            let group = params.group || 0
            let searchStart = 0
            while (true) {
                let match = execWithIndices(regex, blockText.substring(searchStart))
                if (!match)
                    break
                let matchStart = searchStart + match.indices[group][0]
                let matchEnd = searchStart + match.indices[group][1]
                let startPos = editor.document.positionAt(blockOffset + matchStart)
                let endPos = editor.document.positionAt(blockOffset + matchEnd)
                matches.push({
                    startLine: startPos.line,
                    startCol: startPos.character,
                    endLine: endPos.line,
                    endCol: endPos.character
                })
                if (!replaceAll && matches.length >= maxCount)
                    break
                searchStart = matchStart + 1
            }
        }
        else {
            let searchStart = 0
            while (true) {
                let offset = blockText.indexOf(params.match, searchStart)
                if (offset < 0)
                    break
                let startPos = editor.document.positionAt(blockOffset + offset)
                let endPos = editor.document.positionAt(blockOffset + offset + params.match.length)
                matches.push({
                    startLine: startPos.line,
                    startCol: startPos.character,
                    endLine: endPos.line,
                    endCol: endPos.character
                })
                if (!replaceAll && matches.length >= maxCount)
                    break
                searchStart = offset + 1
            }
        }
    }
    else if (params.isRegex) {
        let regex = new RegExp(params.match)
        let group = params.group || 0
        for (let line = startLine; line < stopLine; line++) {
            let currentLine = editor.document.lineAt(line)
            let match = execWithIndices(regex, currentLine.text)
            if (match) {
                matches.push({
                    startLine: line,
                    startCol: match.indices[group][0],
                    endLine: line,
                    endCol: match.indices[group][1]
                })
                if (!replaceAll && matches.length >= maxCount)
                    break
            }
        }
    }
    else {
        for (let line = startLine; line < stopLine; line++) {
            let currentLine = editor.document.lineAt(line)
            let offset = currentLine.text.indexOf(params.match)
            if (offset >= 0) {
                matches.push({
                    startLine: line,
                    startCol: offset,
                    endLine: line,
                    endCol: offset + params.match.length
                })
                if (!replaceAll && matches.length >= maxCount)
                    break
            }
        }
    }

    // Bail out if there were no matches found anywhere in the file.

    if (matches.length === 0)
        return

    // Replace all matched text with the replacement text. Apply in
    // reverse order so line/column offsets remain valid.

    await editor.edit(builder => {
        for (let i = matches.length - 1; i >= 0; i--) {
            let m = matches[i]
            let startPosition = new vscode.Position(m.startLine, m.startCol)
            let stopPosition = new vscode.Position(m.endLine, m.endCol)
            let range = new vscode.Range(startPosition, stopPosition)
            builder.replace(range, params.replacement)
        }
    })

    // Select and reveal the last replacement.

    let last = matches[matches.length - 1]
    let replacementLines = params.replacement.split('\n')
    let startPosition = new vscode.Position(last.startLine, last.startCol)
    let endLine = last.startLine + replacementLines.length - 1
    let endCol = replacementLines.length === 1
        ? last.startCol + replacementLines[0].length
        : replacementLines[replacementLines.length - 1].length
    let stopPosition = new vscode.Position(endLine, endCol)
    editor.selection = new vscode.Selection(startPosition, stopPosition)

    editor.revealRange(editor.selection, vscode.TextEditorRevealType.InCenter)

    await editor.document.save()
}
