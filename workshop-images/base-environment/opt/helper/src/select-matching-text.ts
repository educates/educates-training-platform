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

    let line = 0

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
    else if (stopLine >= lines)
        stopLine = lines - 1

    let startMatch = -1
    let stopMatch = -1

    if (params.isRegex) {
        let regex = new RegExp(params.text)
        let group = params.group || 0
        for (line = startLine; line < stopLine; line++) {
            let currentLine = editor.document.lineAt(line)
            let match = execWithIndices(regex, currentLine.text)
            if (match) {
                startMatch = match.indices[group][0]
                stopMatch = match.indices[group][1]
                break
            }
        }
    }
    else {
        for (line = startLine; line < stopLine; line++) {
            let currentLine = editor.document.lineAt(line)
            let offset = currentLine.text.indexOf(params.text)
            if (offset >= 0) {
                startMatch = offset
                stopMatch = offset + params.text.length
                break
            }
        }
    }

    // Bail out out if there was no match found anywhere in the file.

    if (startMatch == -1)
        return

    // Highlight the matched text in file or the region around it.

    if (params.before === undefined && params.after === undefined) {
        // When no lines before or after marked to be select, we only want
        // to highlight the select text.

        let startPosition = new vscode.Position(line, startMatch)
        let stopPosition = new vscode.Position(line, stopMatch)
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
            linesBefore = line

        if (linesAfter === null || linesAfter < 0)
            linesAfter = lines - line - 1

        let startBeforeLine = line - linesBefore

        if (startBeforeLine < 0)
            startBeforeLine = 0

        let stopAfterLine = line + linesAfter

        if (stopAfterLine >= lines)
            stopAfterLine = lines - 1

        let startPosition = new vscode.Position(startBeforeLine, 0)
        let stopPosition = new vscode.Position(stopAfterLine + 1, 0)
        let selection = new vscode.Selection(startPosition, stopPosition)
        editor.selection = selection
        editor.revealRange(editor.selection, vscode.TextEditorRevealType.InCenter)
    }
}
