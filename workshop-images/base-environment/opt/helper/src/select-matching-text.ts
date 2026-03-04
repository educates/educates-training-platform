import * as vscode from "vscode";

import execWithIndices = require("regexp-match-indices")

export interface SelectMatchingTextParams {
    file: string,
    text: string,
    start?: number,
    stop?: number,
    isRegex?: boolean,
    group?: number,
    before?: number,
    after?: number
}

export async function selectMatchingText(params: SelectMatchingTextParams) {
    // Display the editor window for the target file.

    const doc = await vscode.workspace.openTextDocument(params.file)
    const editor = await vscode.window.showTextDocument(doc)

    // Bail out if there was no text to match provided.

    if (!params.text)
        return

    // Find the matching line based on whether regex or exact match.

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

    let matchStartLine = -1
    let matchStartCol = -1
    let matchEndLine = -1
    let matchEndCol = -1

    // Check whether the text to match spans multiple lines.

    let isMultiLine = params.text.indexOf('\n') >= 0

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
            let regex = new RegExp(params.text)
            let group = params.group || 0
            let match = execWithIndices(regex, blockText)
            if (match) {
                let startPos = editor.document.positionAt(blockOffset + match.indices[group][0])
                let endPos = editor.document.positionAt(blockOffset + match.indices[group][1])
                matchStartLine = startPos.line
                matchStartCol = startPos.character
                matchEndLine = endPos.line
                matchEndCol = endPos.character
            }
        }
        else {
            let offset = blockText.indexOf(params.text)
            if (offset >= 0) {
                let startPos = editor.document.positionAt(blockOffset + offset)
                let endPos = editor.document.positionAt(blockOffset + offset + params.text.length)
                matchStartLine = startPos.line
                matchStartCol = startPos.character
                matchEndLine = endPos.line
                matchEndCol = endPos.character
            }
        }
    }
    else if (params.isRegex) {
        let regex = new RegExp(params.text)
        let group = params.group || 0
        for (let line = startLine; line < stopLine; line++) {
            let currentLine = editor.document.lineAt(line)
            let match = execWithIndices(regex, currentLine.text)
            if (match) {
                matchStartLine = line
                matchStartCol = match.indices[group][0]
                matchEndLine = line
                matchEndCol = match.indices[group][1]
                break
            }
        }
    }
    else {
        for (let line = startLine; line < stopLine; line++) {
            let currentLine = editor.document.lineAt(line)
            let offset = currentLine.text.indexOf(params.text)
            if (offset >= 0) {
                matchStartLine = line
                matchStartCol = offset
                matchEndLine = line
                matchEndCol = offset + params.text.length
                break
            }
        }
    }

    // Bail out out if there was no match found anywhere in the file.

    if (matchStartLine == -1)
        return

    // Highlight the matched text in file or the region around it.

    if (params.before === undefined && params.after === undefined) {
        // When no lines before or after marked to be select, we only want
        // to highlight the matched text.

        let startPosition = new vscode.Position(matchStartLine, matchStartCol)
        let stopPosition = new vscode.Position(matchEndLine, matchEndCol)
        let selection = new vscode.Selection(startPosition, stopPosition)
        editor.selection = selection
        editor.revealRange(editor.selection, vscode.TextEditorRevealType.InCenter)
    }
    else {
        // When lines before or after marked to be select, we always select
        // whole lines.

        let linesBefore = (params.before === undefined) ? 0 : params.before
        let linesAfter = (params.after === undefined) ? 0 : params.after

        // Use negative values to indicate all lines before or after.

        if (linesBefore === null || linesBefore < 0)
            linesBefore = matchStartLine

        if (linesAfter === null || linesAfter < 0)
            linesAfter = lines - matchEndLine - 1

        let startBeforeLine = matchStartLine - linesBefore

        if (startBeforeLine < 0)
            startBeforeLine = 0

        let stopAfterLine = matchEndLine + linesAfter

        if (stopAfterLine >= lines)
            stopAfterLine = lines - 1

        let startPosition = new vscode.Position(startBeforeLine, 0)
        let stopPosition = new vscode.Position(stopAfterLine + 1, 0)
        let selection = new vscode.Selection(startPosition, stopPosition)
        editor.selection = selection
        editor.revealRange(editor.selection, vscode.TextEditorRevealType.InCenter)
    }
}
