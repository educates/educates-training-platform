const educates = (function () {
    // Function to copy text to clipboard
    function set_paste_buffer_to_text(text) {
        navigator.clipboard.writeText(text).catch(err => {
            console.error('Failed to copy text: ', err);
        });
    }

    // The Terminals class is a stub implementation which will be replaced
    // by the parent frame's terminals object if it exists. The stub will be
    // used when workshop pages are viewed as standalone pages.

    class Terminals {
        paste_to_terminal(text, session) {
            console.log('paste_to_terminal:', text, session);
        }

        paste_to_all_terminals(text) {
            console.log('paste_to_all_terminals:', text);
        }

        execute_in_terminal(command, session, clear) {
            console.log('execute_in_terminal:', command, session, clear);
        }

        execute_in_all_terminals(command, clear) {
            console.log('execute_in_all_terminals:', command, clear);
        }

        select_terminal(session) {
            console.log('select_terminal:', session);
            return true;
        }

        clear_terminal(session) {
            console.log('clear_terminal:', session);
        }

        clear_all_terminals() {
            console.log('clear_all_terminals');
        }

        interrupt_terminal(session) {
            console.log('interrupt_terminal:', session);
        }

        interrupt_all_terminals() {
            console.log('interrupt_all_terminals');
        }
    }

    var terminals = new Terminals();

    if (parent && parent.educates && parent.educates.terminals) {
        terminals = parent.educates.terminals;
    }

    // The Dashboard class is a stub implementation which will be replaced
    // by the parent frame's dashboard object if it exists. The stub will be
    // used when workshop pages are viewed as standalone pages.

    class Dashboard {
        session_owner() {
            console.log('session_owner');
            return "educates";
        }

        expose_terminal(session) {
            console.log('expose_terminal:', session);
            return true;
        }

        expose_dashboard(name) {
            console.log('expose_dashboard:', name);
            return true;
        }

        create_dashboard(name, url, focus) {
            console.log('create_dashboard:', name, url, focus);
            return true;
        }

        delete_dashboard(name) {
            console.log('delete_dashboard:', name);
            return true;
        }

        reload_dashboard(name, url, focus) {
            console.log('reload_dashboard:', name, url, focus);
            return true;
        }

        collapse_workshop() {
            console.log('collapse_workshop');
        }

        reload_workshop() {
            console.log('reload_workshop');
        }

        finished_workshop() {
            console.log('finished_workshop');
        }

        terminate_session() {
            console.log('terminate_session');
        }

        preview_image(src, title) {
            // Pop out images when clicked in a modal dialog.

            const preview_element = document.getElementById('preview-image-element');
            const preview_title = document.getElementById('preview-image-title');
            const preview_dialog = document.getElementById('preview-image-dialog');

            if (preview_element && preview_title && preview_dialog) {
                preview_element.setAttribute('src', src);
                preview_title.textContent = title;
                const modal = new bootstrap.Modal(preview_dialog);
                modal.show();
            }
        }
    }

    var dashboard = new Dashboard();

    if (parent && parent.educates && parent.educates.dashboard) {
        dashboard = parent.educates.dashboard;
    }

    // The Examiner class implements examiner test execution functionality.
    // The parent frame doesn't currently provide an examiner object, so
    // everything is implemented here, with caveat that we don't do anything
    // if we are running as a standalone page.

    class Examiner {
        execute_test(name, options = {}) {
            console.log('execute_test:', name, options);

            if (!parent || !parent.educates || !parent.educates.dashboard) {
                return Promise.reject(new Error('Examiner not available in standalone mode'));
            }

            const {
                url = null,
                args = [],
                form = null,
                timeout = 30,
                retries = 0,
                delay = 1
            } = options;

            if (!name) {
                return Promise.reject(new Error('Test name not provided'));
            }

            const endpoint = url || `/examiner/test/${name}`;
            const body = JSON.stringify({ args, form, timeout });

            const attempt_call = (remaining_retries) => {
                return fetch(endpoint, {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json'
                    },
                    body: body
                })
                .then(response => {
                    if (!response.ok) {
                        throw new Error('Unexpected HTTP error');
                    }
                    return response.json();
                })
                .then(result => {
                    if (!result.success) {
                        if (remaining_retries > 0) {
                            return new Promise(resolve => {
                                setTimeout(() => {
                                    resolve(attempt_call(remaining_retries - 1));
                                }, delay * 1000);
                            });
                        }
                        throw new Error(result.message || 'Test failed');
                    }
                    return result;
                })
                .catch(error => {
                    if (remaining_retries > 0) {
                        return new Promise(resolve => {
                            setTimeout(() => {
                                resolve(attempt_call(remaining_retries - 1));
                            }, delay * 1000);
                        });
                    }
                    throw error;
                });
            };

            return attempt_call(retries);
        }
    }

    var examiner = new Examiner();

    if (parent && parent.educates && parent.educates.examiner) {
        examiner = parent.educates.examiner;
    }

    // Setup everything when the DOM is ready.

    document.addEventListener('DOMContentLoaded', function () {
        // Attach event listeners to all inline-copy elements.
    
        const elements = document.querySelectorAll('.inline-copy');

        elements.forEach(element => {
            // Find the preceding code element.

            const target = element.previousElementSibling;

            if (target && target.tagName === 'CODE') {
                // Add click event listener to the code element.

                target.addEventListener('click', () => {
                    // Copy the text content.

                    set_paste_buffer_to_text(target.textContent);

                    // Update the icon classes.

                    element.classList.add('fas');
                    element.classList.remove('far');

                    // Reset the icon after 250ms.

                    setTimeout(() => {
                        element.classList.add('far');
                        element.classList.remove('fas');
                    }, 250);
                });
            }
        });

        // Handle external links in page content.

        const links = document.querySelectorAll('section.page-content a');

        links.forEach(anchor => {
            if (!(location.hostname === anchor.hostname || !anchor.hostname.length)) {
                anchor.setAttribute('target', '_blank');
            }
        });

        // Handle image preview in page content.

        const images = document.querySelectorAll('section.page-content img');

        images.forEach(image => {
            image.addEventListener('click', () => {
                dashboard.preview_image(image.src, image.alt);
            });
        });
    });

    // Table of clickable actions and their handlers. Clickable actions will
    // be registered on page load and each have a unique incrementing integer
    // ID appended to "clickable-action-" as their element ID.

    const clickable_action_handlers = {};
    const clickable_actions = {};

    // Shift+click copy functionality: track shift key state and hovered action.

    let shift_key_pressed = false;
    let hovered_clickable_action = null;

    // Helper function to show copy icon on a clickable action element.

    function show_copy_icon(element) {
        // Don't show copy icon if action is pending.

        if (element.dataset.actionResult === 'pending') {
            return;
        }

        const glyph_element = element.querySelector('.clickable-action__icon');
        const original_glyph = element.dataset.originalGlyph;

        if (glyph_element && original_glyph) {
            // Also need to remove success icon if present.

            glyph_element.classList.remove(original_glyph, 'fa-check-circle');
            glyph_element.classList.add('fa-copy');
        }
    }

    // Helper function to restore icon on a clickable action element based on state.

    function restore_original_icon(element) {
        const glyph_element = element.querySelector('.clickable-action__icon');
        const original_glyph = element.dataset.originalGlyph;
        const action_result = element.dataset.actionResult;

        if (!glyph_element || !original_glyph) {
            return;
        }

        // Don't restore if action is pending (shouldn't have shown copy icon).

        if (action_result === 'pending') {
            return;
        }

        glyph_element.classList.remove('fa-copy');

        // Restore to appropriate icon based on action state.

        if (action_result === 'success') {
            glyph_element.classList.add('fa-check-circle');
        } else {
            // Idle or failure state - restore original icon.
            glyph_element.classList.add(original_glyph);
        }
    }

    // Find the clickable action element currently under the mouse cursor.

    function find_hovered_clickable_action() {
        // Check all registered clickable actions for :hover state.

        for (const action_id in clickable_actions) {
            const element = clickable_actions[action_id].element;
            if (element.matches(':hover')) {
                return element;
            }
        }

        return null;
    }

    // Update icon on currently hovered action when shift is pressed.

    function update_hovered_action_icon() {
        // Actively detect hovered element in case mouseenter hasn't fired yet.

        if (!hovered_clickable_action) {
            hovered_clickable_action = find_hovered_clickable_action();
        }

        if (hovered_clickable_action) {
            show_copy_icon(hovered_clickable_action);
        }
    }

    // Restore icon on currently hovered action when shift is released.

    function restore_hovered_action_icon() {
        if (hovered_clickable_action) {
            restore_original_icon(hovered_clickable_action);
        }
    }

    // Show visual feedback after successful copy operation.

    function show_copy_feedback(element) {
        const glyph_element = element.querySelector('.clickable-action__icon');

        if (glyph_element) {
            // Show clipboard-check briefly to indicate successful copy.

            glyph_element.classList.remove('fa-copy');
            glyph_element.classList.add('fa-clipboard-check');

            setTimeout(() => {
                glyph_element.classList.remove('fa-clipboard-check');

                if (shift_key_pressed && hovered_clickable_action === element) {
                    glyph_element.classList.add('fa-copy');
                } else {
                    // Restore appropriate icon based on action state.

                    const action_result = element.dataset.actionResult;

                    if (action_result === 'success') {
                        glyph_element.classList.add('fa-check-circle');
                    } else {
                        const original_glyph = element.dataset.originalGlyph;
                        if (original_glyph) {
                            glyph_element.classList.add(original_glyph);
                        }
                    }
                }
            }, 250);
        }
    }

    // Set up global shift key event listeners.

    document.addEventListener('keydown', (event) => {
        if (event.key === 'Shift' && !shift_key_pressed) {
            shift_key_pressed = true;
            update_hovered_action_icon();
        }
    });

    document.addEventListener('keyup', (event) => {
        if (event.key === 'Shift') {
            shift_key_pressed = false;
            restore_hovered_action_icon();
        }
    });

    // Action state constants for centralized state management.

    const ActionState = {
        IDLE: 'idle',
        PENDING: 'pending',
        SUCCESS: 'success',
        FAILURE: 'failure'
    };

    // Centralized function to manage action visual state transitions.

    function set_action_state(element, state, error = null) {
        const glyph_element = element.querySelector('.clickable-action__icon');
        const original_glyph = element.dataset.originalGlyph;

        // Remove all state classes from glyph element if it exists.

        if (glyph_element) {
            glyph_element.classList.remove('fa-spin', 'fa-cog', 'fa-check-circle', 'fa-times-circle');
        }

        switch (state) {
            case ActionState.PENDING:
                element.dataset.actionResult = 'pending';
                if (glyph_element) {
                    if (original_glyph) {
                        glyph_element.classList.remove(original_glyph);
                    }
                    glyph_element.classList.add('fa-cog', 'fa-spin');
                }
                break;

            case ActionState.SUCCESS:
                element.dataset.actionResult = 'success';
                element.dataset.actionCompleted = Date.now().toString();
                if (glyph_element) {
                    if (original_glyph) {
                        glyph_element.classList.remove(original_glyph);
                    }
                    glyph_element.classList.remove('fa-cog', 'fa-spin');
                    glyph_element.classList.add('fa-check-circle');
                }
                break;

            case ActionState.FAILURE:
                element.dataset.actionResult = 'failure';
                element.dataset.actionCompleted = Date.now().toString();
                if (glyph_element) {
                    glyph_element.classList.remove('fa-cog', 'fa-spin');
                    if (original_glyph) {
                        glyph_element.classList.add(original_glyph);
                    }
                }
                if (error) {
                    console.error(`Action failed: ${error.message || error}`);
                }
                break;

            case ActionState.IDLE:
            default:
                element.dataset.actionResult = '';
                if (glyph_element && original_glyph) {
                    glyph_element.classList.add(original_glyph);
                }
                break;
        }
    }

    // Cooldown check to prevent rapid re-triggering of actions.

    const ACTION_COOLDOWN_MS = 1000;

    function check_cooldown(element, cooldown_ms) {
        const last_completed = element.dataset.actionCompleted;

        if (!last_completed) {
            return true;
        }

        const elapsed = Date.now() - parseInt(last_completed, 10);

        return elapsed >= cooldown_ms;
    }

    // Default timeout for action execution in milliseconds.

    const ACTION_TIMEOUT_MS = 30000;

    function register_clickable_action(action, args) {
        const element = document.getElementById(action);
        const handler = element.dataset.handler;

        console.log("register_clickable_action", handler, action);

        // Store the glyph element's original icon class for state restoration.

        const glyph_element = element.querySelector('.clickable-action__icon');

        if (glyph_element) {
            // Find the FontAwesome icon class (fa-*) that isn't a modifier.

            const icon_classes = Array.from(glyph_element.classList).filter(cls =>
                cls.startsWith('fa-') && !['fa-spin', 'fa-cog', 'fa-check-circle', 'fa-times-circle'].includes(cls)
            );

            if (icon_classes.length > 0) {
                element.dataset.originalGlyph = icon_classes[0];
            }
        }

        // Store the action configuration for later execution.

        clickable_actions[action] = {
            element: element,
            args: args,
            handler: handler
        };

        // Call setup callback if defined for this handler type.

        const handler_config = clickable_action_handlers[handler];

        if (handler_config && handler_config.setup) {
            try {
                handler_config.setup(element, args);
            } catch (error) {
                console.error(`Setup callback failed for ${handler}:`, error);
            }
        }

        // Add hover listeners for shift+click copy functionality.

        element.addEventListener('mouseenter', () => {
            hovered_clickable_action = element;
            if (shift_key_pressed) {
                show_copy_icon(element);
            }
        });

        element.addEventListener('mouseleave', () => {
            if (hovered_clickable_action === element) {
                restore_original_icon(element);
                hovered_clickable_action = null;
            }
        });
    }

    // Execute an action with promise-based handling and timeout support.

    async function execute_action(action_id) {
        const action_config = clickable_actions[action_id];

        if (!action_config) {
            console.error(`No action registered for: ${action_id}`);
            return;
        }

        const { element, args, handler: handler_name } = action_config;
        const handler_config = clickable_action_handlers[handler_name];

        if (!handler_config || !handler_config.handler) {
            console.error(`No handler registered for: ${handler_name}`);
            return;
        }

        // Don't allow re-triggering while action is pending.

        if (element.dataset.actionResult === 'pending') {
            console.log(`Action ${action_id} already pending`);
            return;
        }

        // Check cooldown to prevent rapid re-triggering. Get cooldown from args
        // (seconds) and convert to ms, or use global default.

        const cooldown_ms = args.cooldown !== undefined ? args.cooldown * 1000 : ACTION_COOLDOWN_MS;

        if (!check_cooldown(element, cooldown_ms)) {
            console.log(`Action ${action_id} in cooldown period`);
            return;
        }

        // Helper to call finish callback safely (supports async).

        async function call_finish_callback(state, error) {
            if (handler_config.finish) {
                try {
                    const result = handler_config.finish(element, args, state, error);
                    if (result instanceof Promise) {
                        await result;
                    }
                } catch (finishError) {
                    console.error(`Finish callback failed for ${handler_name}:`, finishError);
                }
            }
        }

        // In standalone mode, show visual feedback but don't execute the action.
        // Note: finish callback is NOT called in standalone mode.

        if (!parent || !parent.educates || !parent.educates.dashboard) {
            console.log(`Action ${action_id} triggered in standalone mode`);
            set_action_state(element, ActionState.SUCCESS);
            return;
        }

        // Set pending state.

        set_action_state(element, ActionState.PENDING);

        try {
            // Execute handler - await if it returns a promise.

            const result = handler_config.handler(element, args);

            if (result instanceof Promise) {
                // Apply timeout using Promise.race.

                const timeout = args.timeout || ACTION_TIMEOUT_MS;

                await Promise.race([
                    result,
                    new Promise((_, reject) =>
                        setTimeout(() => reject(new Error('Action timed out')), timeout)
                    )
                ]);
            }

            // Success.

            set_action_state(element, ActionState.SUCCESS);

            // Call finish callback and wait for it to complete.

            await call_finish_callback(ActionState.SUCCESS, null);

            // Handle cascade if configured (after finish callback completes).

            if (args.cascade) {
                const pause = args.pause || 750;
                setTimeout(() => trigger_next_action(element), pause);
            }

        } catch (error) {
            // Failure.

            set_action_state(element, ActionState.FAILURE, error);

            // Call finish callback on failure too.

            await call_finish_callback(ActionState.FAILURE, error);
        }
    }

    // Placeholder for cascade functionality.

    function trigger_next_action(element) {
        // Find the next sibling action element and trigger it.

        const next_action = element.nextElementSibling;

        if (next_action && next_action.classList.contains('magic-code-block')) {
            const action_id = next_action.id;

            if (action_id && clickable_actions[action_id]) {
                execute_action(action_id);
            }
        }
    }

    function trigger_clickable_action(event) {
        const element = event.currentTarget;
        const action = element.id;

        // If shift key is pressed, copy inner text instead of executing action.
        // But not if action is currently pending.

        if (event.shiftKey && element.dataset.actionResult !== 'pending') {
            const body_element = element.querySelector('.clickable-action__body');
            if (body_element) {
                set_paste_buffer_to_text(body_element.textContent);
                show_copy_feedback(element);

                // Clear any text selection caused by shift+click.

                window.getSelection().removeAllRanges();
            }
            return;
        }

        console.log("trigger_clickable_action", element.dataset.handler, action);

        execute_action(action);
    }

    function clickable_action_handler(name, callbacks) {
        clickable_action_handlers[name] = {
            setup: callbacks.setup || null,
            handler: callbacks.handler || null,
            finish: callbacks.finish || null
        };
    }

    // Register built-in clickable action handlers.

    clickable_action_handler("terminal:execute", {
        handler: function (_element, args) {
            const defaults = {
                "command": undefined,
                "session": "1",
                "clear": false,
            }

            args = { ...defaults, ...args }

            const command = args.command;
            const session = args.session || "1";
            const clear = args.clear;

            if (!command) {
                throw new Error("Command not provided");
            }

            terminals.execute_in_terminal(command, session, clear);
        }
    });

    clickable_action_handler("terminal:execute-all", {
        handler: function (_element, args) {
            const defaults = {
                "command": undefined,
                "clear": false,
            }

            args = { ...defaults, ...args }

            const command = args.command;
            const clear = args.clear;

            if (!command) {
                throw new Error("Command not provided");
            }

            terminals.execute_in_all_terminals(command, clear);
        }
    });

    clickable_action_handler("terminal:interrupt", {
        handler: function (_element, args) {
            const defaults = {
                "session": "1",
            }

            args = { ...defaults, ...args }

            const session = args.session;

            terminals.interrupt_terminal(session);
        }
    });

    clickable_action_handler("terminal:interrupt-all", {
        handler: function (_element, _args) {
            terminals.interrupt_all_terminals();
        }
    });

    clickable_action_handler("terminal:clear", {
        handler: function (_element, args) {
            const defaults = {
                "session": "1",
            }

            args = { ...defaults, ...args }

            const session = args.session;

            terminals.clear_terminal(session);
        }
    });

    clickable_action_handler("terminal:clear-all", {
        handler: function (_element, _args) {
            terminals.clear_all_terminals();
        }
    });

    clickable_action_handler("terminal:input", {
        handler: function (_element, args) {
            console.log("terminal:input handler called", args);
        }
    });

    clickable_action_handler("terminal:select", {
        handler: function (_element, args) {
            console.log("terminal:select handler called", args);
        }
    });

    clickable_action_handler("workshop:copy", {
        handler: function (_element, args) {
            console.log("workshop:copy handler called", args);
        }
    });

    clickable_action_handler("workshop:copy-and-edit", {
        handler: function (_element, args) {
            console.log("workshop:copy-and-edit handler called", args);
        }
    });

    clickable_action_handler("dashboard:expose-dashboard", {
        handler: function (_element, args) {
            console.log("dashboard:expose-dashboard handler called", args);
        }
    });

    clickable_action_handler("dashboard:open-dashboard", {
        handler: function (_element, args) {
            console.log("dashboard:open-dashboard handler called", args);
        }
    });

    clickable_action_handler("dashboard:create-dashboard", {
        handler: function (_element, args) {
            console.log("dashboard:create-dashboard handler called", args);
        }
    });

    clickable_action_handler("dashboard:delete-dashboard", {
        handler: function (_element, args) {
            console.log("dashboard:delete-dashboard handler called", args);
        }
    });

    clickable_action_handler("dashboard:reload-dashboard", {
        handler: function (_element, args) {
            console.log("dashboard:reload-dashboard handler called", args);
        }
    });

    clickable_action_handler("dashboard:open-url", {
        handler: function (_element, args) {
            console.log("dashboard:open-url handler called", args);
        }
    });

    clickable_action_handler("editor:open-file", {
        handler: function (_element, args) {
            console.log("editor:open-file handler called", args);
        }
    });

    clickable_action_handler("editor:select-matching-text", {
        handler: function (_element, args) {
            console.log("editor:select-matching-text handler called", args);
        }
    });

    clickable_action_handler("editor:replace-text-selection", {
        handler: function (_element, args) {
            console.log("editor:replace-text-selection handler called", args);
        }
    });

    clickable_action_handler("editor:append-lines-to-file", {
        handler: function (_element, args) {
            console.log("editor:append-lines-to-file handler called", args);
        }
    });

    clickable_action_handler("editor:insert-lines-before-line", {
        handler: function (_element, args) {
            console.log("editor:insert-lines-before-line handler called", args);
        }
    });

    clickable_action_handler("editor:append-lines-after-match", {
        handler: function (_element, args) {
            console.log("editor:append-lines-after-match handler called", args);
        }
    });

    clickable_action_handler("editor:insert-value-into-yaml", {
        handler: function (_element, args) {
            console.log("editor:insert-value-into-yaml handler called", args);
        }
    });

    clickable_action_handler("editor:execute-command", {
        handler: function (_element, args) {
            console.log("editor:execute-command handler called", args);
        }
    });

    clickable_action_handler("examiner:execute-test", {
        handler: function (_element, args) {
            console.log("examiner:execute-test handler called", args);

            const defaults = {
                "name": undefined,
                "args": [],
                "timeout": 30,
                "retries": 0,
                "delay": 1
            };

            args = { ...defaults, ...args };

            if (!args.name) {
                throw new Error("Test name not provided");
            }

            return examiner.execute_test(args.name, {
                args: args.args,
                timeout: args.timeout,
                retries: args.retries,
                delay: args.delay
            });
        }
    });

    clickable_action_handler("files:download-file", {
        handler: function (_element, args) {
            console.log("files:download-file handler called", args);
        }
    });

    clickable_action_handler("files:copy-file", {
        handler: function (_element, args) {
            console.log("files:copy-file handler called", args);
        }
    });

    clickable_action_handler("files:upload-file", {
        handler: function (_element, args) {
            console.log("files:upload-file handler called", args);
        }
    });

    clickable_action_handler("files:upload-files", {
        handler: function (_element, args) {
            console.log("files:upload-files handler called", args);
        }
    });

    clickable_action_handler("section:heading", {
        handler: function (_element, args) {
            console.log("section:heading handler called", args);
        }
    });

    clickable_action_handler("section:begin", {
        handler: function (_element, args) {
            console.log("section:begin handler called", args);
        }
    });

    clickable_action_handler("section:end", {
        handler: function (_element, args) {
            console.log("section:end handler called", args);
        }
    });

    // Exported functions.

    function paste_to_terminal(text, session) {
        session = session || "1";

        if (session == "*") {
            if (!dashboard.expose_dashboard("terminal")) {
                return false;
            }
            terminals.paste_to_all_terminals(text);
        } else {
            if (!dashboard.expose_terminal(session)) {
                return false;
            }
            terminals.paste_to_terminal(text, session);
            return true;
        }
    }

    function paste_to_all_terminals(text) {
        if (!dashboard.expose_dashboard("terminal")) {
            return false;
        }
        terminals.paste_to_all_terminals(text);
        return true;
    }

    function execute_in_terminal(command, session, clear = false) {
        session = session || "1";

        if (session == "*") {
            if (!dashboard.expose_dashboard("terminal")) {
                return false;
            }
            terminals.execute_in_all_terminals(command, clear);
        } else {
            if (!dashboard.expose_terminal(session)) {
                return false;
            }
            terminals.execute_in_terminal(command, session, clear);
        }
        return true;
    }

    function execute_in_all_terminals(command, clear = false) {
        if (!dashboard.expose_dashboard("terminal")) {
            return false;
        }
        terminals.execute_in_all_terminals(command, clear);
        return true;
    }

    function clear_terminal(session) {
        session = session || "1";

        if (session == "*") {
            if (!dashboard.expose_dashboard("terminal")) {
                return false;
            }
            terminals.clear_all_terminals();
        } else {
            if (!dashboard.expose_terminal(session)) {
                return false;
            }
            terminals.clear_terminal(session);
        }
        return true;
    }

    function clear_all_terminals() {
        if (!dashboard.expose_dashboard("terminal")) {
            return false;
        }
        terminals.clear_all_terminals();
        return true;
    }

    function interrupt_terminal(session) {
        session = session || "1";

        if (session == "*") {
            if (!dashboard.expose_dashboard("terminal")) {
                return false;
            }
            terminals.interrupt_all_terminals();
        } else {
            if (!dashboard.expose_terminal(session)) {
                return false;
            }
            terminals.interrupt_terminal(session);
        }
        return true;
    }

    function interrupt_all_terminals() {
        terminals.interrupt_all_terminals();
    }

    function expose_terminal(session) {
        if (!dashboard.expose_terminal(session)) {
            return false;
        }
        terminals.select_terminal(session);
        return true;
    }

    function expose_dashboard(name) {
        return dashboard.expose_dashboard(name);
    }

    function create_dashboard(name, url, focus) {
        return dashboard.create_dashboard(name, url, focus);
    }

    function delete_dashboard(name) {
        return dashboard.delete_dashboard(name);
    }

    function reload_dashboard(name, url, focus) {
        return dashboard.reload_dashboard(name, url, focus);
    }

    function collapse_workshop() {
        dashboard.collapse_workshop();
    }

    function reload_workshop() {
        dashboard.reload_workshop();
    }

    function finished_workshop() {
        dashboard.finished_workshop();
    }

    function terminate_session() {
        dashboard.terminate_session();
    }

    function preview_image(src, title) {
        dashboard.preview_image(src, title);
    }

    return {
        register_clickable_action,
        trigger_clickable_action,
        paste_to_terminal,
        paste_to_all_terminals,
        execute_in_terminal,
        execute_in_all_terminals,
        clear_terminal,
        clear_all_terminals,
        interrupt_terminal,
        interrupt_all_terminals,
        expose_terminal,
        expose_dashboard,
        create_dashboard,
        delete_dashboard,
        reload_dashboard,
        collapse_workshop,
        reload_workshop,
        finished_workshop,
        terminate_session,
        preview_image,
    };
})();

window.educates = educates;
