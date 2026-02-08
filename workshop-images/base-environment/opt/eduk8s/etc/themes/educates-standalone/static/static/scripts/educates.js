const educates = (function () {
    // Function to copy text to clipboard. Uses the modern Clipboard API with
    // fallback to the deprecated execCommand method for browsers that don't
    // support it or when the Clipboard API fails due to permissions.

    function set_paste_buffer_to_text(text) {
        function fallback_copy(text) {
            const textarea = document.createElement('textarea');

            textarea.value = text;
            textarea.style.position = 'fixed';
            textarea.style.left = '-9999px';
            textarea.style.top = '-9999px';

            document.body.appendChild(textarea);
            textarea.focus();
            textarea.select();

            try {
                document.execCommand('copy');
            } catch (err) {
                console.error('Fallback copy failed:', err);
            }

            document.body.removeChild(textarea);
        }

        if (navigator.clipboard && navigator.clipboard.writeText) {
            navigator.clipboard.writeText(text).catch(err => {
                console.warn('Clipboard API failed, using fallback:', err);
                fallback_copy(text);
            });
        } else {
            fallback_copy(text);
        }
    }

    // Function to send analytics events to various consumers (webhook, Google
    // Analytics, Amplitude). Events are sent asynchronously with optional timeout.

    async function send_analytics_event(event, data = {}, timeout = 0) {
        // Return early if not in an iframe or parent doesn't provide educates.dashboard.

        if (!parent || !parent.educates || !parent.educates.dashboard) {
            return;
        }

        const payload = {
            event: {
                name: event,
                data: data
            }
        };

        console.log('Sending analytics event:', JSON.stringify(payload));

        const body = document.body;

        const send_to_webhook = function () {
            return fetch('/session/event', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(payload)
            }).then(response => {
                if (!response.ok) {
                    throw new Error(`HTTP error ${response.status}`);
                }
                return response.json();
            });
        };

        const tasks = [send_to_webhook().catch(err => {
            console.error('Failed to send analytics event to webhook:', err);
        })];

        if (body.dataset.googleTrackingId && typeof gtag !== 'undefined') {
            const send_to_google = function () {
                return new Promise((resolve) => {
                    const callbacks = {
                        'event_callback': (arg) => resolve(arg)
                    };

                    gtag('event', event, Object.assign({}, callbacks, data));
                });
            };

            tasks.push(send_to_google().catch(err => {
                console.error('Failed to send analytics event to Google:', err);
            }));
        }

        if (body.dataset.amplitudeTrackingId && typeof amplitude !== 'undefined') {
            const send_to_amplitude = function () {
                const globals = {
                    'workshop_name': body.dataset.workshopName,
                    'session_name': body.dataset.sessionNamespace,
                    'environment_name': body.dataset.workshopNamespace,
                    'training_portal': body.dataset.trainingPortal,
                    'ingress_domain': body.dataset.ingressDomain,
                    'ingress_protocol': body.dataset.ingressProtocol,
                    'session_owner': dashboard.session_owner(),
                };

                return amplitude.track(event, Object.assign({}, globals, data)).promise;
            };

            tasks.push(send_to_amplitude().catch(err => {
                console.error('Failed to send analytics event to Amplitude:', err);
            }));
        }

        function abort_after_ms(ms) {
            return new Promise(resolve => setTimeout(resolve, ms));
        }

        if (timeout) {
            try {
                await Promise.race([
                    Promise.all(tasks),
                    abort_after_ms(timeout)
                ]);
            }
            catch (err) {
                console.log('Error sending analytics event', event, err);
            }
        }
        else {
            Promise.all(tasks).catch(err => {
                console.log('Error sending analytics event', event, err);
            });
        }
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

    // The Editor class provides integration with VSCode/code-server for file
    // editing operations. Unlike terminals and dashboard, the editor is accessed
    // directly via HTTP API calls rather than through the parent frame.

    class Editor {
        constructor() {
            this.url = null;
            this.retries = 25;
            this.retry_delay = 1000;

            // Try to get configuration from body data attributes.

            const body = document.body;
            const session_namespace = body.dataset.sessionNamespace;
            const ingress_domain = body.dataset.ingressDomain;
            const ingress_protocol = body.dataset.ingressProtocol;
            const ingress_port_suffix = body.dataset.ingressPortSuffix || '';

            if (session_namespace && ingress_domain && ingress_protocol) {
                this.url = `${ingress_protocol}://${session_namespace}.${ingress_domain}${ingress_port_suffix}/code-server`;
            }
        }

        // Normalize file paths to absolute paths in the home directory.

        fixup_path(file) {
            if (file.startsWith('~/')) {
                return file.replace('~/', '/home/eduk8s/');
            } else if (file.startsWith('$HOME/')) {
                return file.replace('$HOME/', '/home/eduk8s/');
            } else if (!file.startsWith('/')) {
                return '/home/eduk8s/' + file;
            }
            return file;
        }

        // Execute an API call to the editor with retry support for 504 errors.

        execute_call(endpoint, data) {
            if (!this.url) {
                return Promise.reject(new Error('Editor not available'));
            }

            const url = this.url + endpoint;
            let remaining_retries = this.retries;
            const retry_delay = this.retry_delay;

            const attempt_call = () => {
                return fetch(url, {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json'
                    },
                    body: data
                })
                    .then(response => {
                        if (!response.ok) {
                            if (response.status === 504 && remaining_retries > 0) {
                                remaining_retries--;
                                return new Promise(resolve => {
                                    setTimeout(() => resolve(attempt_call()), retry_delay);
                                });
                            }
                            throw new Error(`HTTP error ${response.status}`);
                        }
                        return response.text();
                    })
                    .catch(error => {
                        if (remaining_retries > 0) {
                            remaining_retries--;
                            return new Promise(resolve => {
                                setTimeout(() => resolve(attempt_call()), retry_delay);
                            });
                        }
                        throw error;
                    });
            };

            return attempt_call();
        }

        // Open a file in the editor at the specified line.

        open_file(file, line = 1) {
            if (!file) {
                return Promise.reject(new Error('No file name provided'));
            }

            file = this.fixup_path(file);
            const data = JSON.stringify({ file, line });
            return this.execute_call('/editor/line', data);
        }

        // Select matching text in a file. Supports regex patterns with groups.

        select_matching_text(file, text, start, stop, isRegex, group, before, after) {
            if (!file) {
                return Promise.reject(new Error('No file name provided'));
            }

            if (!text) {
                return Promise.reject(new Error('No text to match provided'));
            }

            file = this.fixup_path(file);
            const data = JSON.stringify({ file, text, start, stop, isRegex, group, before, after });
            return this.execute_call('/editor/select-matching-text', data);
        }

        // Replace the current text selection with new text.

        replace_text_selection(file, text) {
            if (!file) {
                return Promise.reject(new Error('No file name provided'));
            }

            if (text === undefined) {
                return Promise.reject(new Error('No replacement text provided'));
            }

            file = this.fixup_path(file);
            const data = JSON.stringify({ file, text });
            return this.execute_call('/editor/replace-text-selection', data);
        }

        // Append lines to the end of a file.

        append_lines_to_file(file, text) {
            if (!file) {
                return Promise.reject(new Error('No file name provided'));
            }

            file = this.fixup_path(file);
            const data = JSON.stringify({ file, paste: text });
            return this.execute_call('/editor/paste', data);
        }

        // Insert lines before a specific line number.

        insert_lines_before_line(file, line, text) {
            if (!file) {
                return Promise.reject(new Error('No file name provided'));
            }

            file = this.fixup_path(file);
            const data = JSON.stringify({ file, line, paste: text });
            return this.execute_call('/editor/paste', data);
        }

        // Append lines after a matching string.

        append_lines_after_match(file, match, text) {
            if (!file) {
                return Promise.reject(new Error('No file name provided'));
            }

            if (!match) {
                return Promise.reject(new Error('No string to match provided'));
            }

            file = this.fixup_path(file);
            const data = JSON.stringify({ file, prefix: match, paste: text });
            return this.execute_call('/editor/paste', data);
        }

        // Insert a value into a YAML file at a specified path.

        insert_value_into_yaml(file, path, value) {
            if (!file) {
                return Promise.reject(new Error('No file name provided'));
            }

            if (!path) {
                return Promise.reject(new Error('No property path provided'));
            }

            if (value === undefined) {
                return Promise.reject(new Error('No property value provided'));
            }

            file = this.fixup_path(file);

            // Convert value to YAML format. Use js-yaml if available, otherwise
            // fall back to a simple conversion.

            let yaml_value;

            if (typeof jsyaml !== 'undefined' && jsyaml.dump) {
                yaml_value = jsyaml.dump(value);
            } else {
                yaml_value = this.simple_yaml_dump(value);
            }

            const data = JSON.stringify({ file, yamlPath: path, paste: yaml_value });
            return this.execute_call('/editor/paste', data);
        }

        // Simple YAML serialization for basic types when js-yaml is unavailable.

        simple_yaml_dump(value, indent = 0) {
            const prefix = '  '.repeat(indent);

            if (value === null || value === undefined) {
                return 'null';
            }

            if (typeof value === 'boolean') {
                return value ? 'true' : 'false';
            }

            if (typeof value === 'number') {
                return String(value);
            }

            if (typeof value === 'string') {
                // Check if string needs quoting.
                if (value === '' || /[:\[\]{}#&*!|>'"%@`]/.test(value) ||
                    value.includes('\n') || /^\s|\s$/.test(value)) {
                    return JSON.stringify(value);
                }
                return value;
            }

            if (Array.isArray(value)) {
                if (value.length === 0) {
                    return '[]';
                }
                return value.map(item => {
                    const dumped = this.simple_yaml_dump(item, indent + 1);
                    if (typeof item === 'object' && item !== null) {
                        return `${prefix}- ${dumped.trimStart()}`;
                    }
                    return `${prefix}- ${dumped}`;
                }).join('\n');
            }

            if (typeof value === 'object') {
                const keys = Object.keys(value);
                if (keys.length === 0) {
                    return '{}';
                }
                return keys.map(key => {
                    const val = value[key];
                    const dumped = this.simple_yaml_dump(val, indent + 1);
                    if (typeof val === 'object' && val !== null && !Array.isArray(val)) {
                        return `${prefix}${key}:\n${dumped}`;
                    } else if (Array.isArray(val)) {
                        return `${prefix}${key}:\n${dumped}`;
                    }
                    return `${prefix}${key}: ${dumped}`;
                }).join('\n');
            }

            return String(value);
        }

        // Execute an editor command with optional arguments.

        execute_command(command, args = []) {
            if (!command) {
                return Promise.reject(new Error('No command provided'));
            }

            const data = JSON.stringify(args);
            return this.execute_call('/command/' + encodeURIComponent(command), data);
        }
    }

    var editor = new Editor();

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

        // Auto-trigger clickable actions with autostart attribute. Note that
        // any which are contained within the body of a section are excluded
        // and will only be executed if the section is revealed.

        const autostart_actions = document.querySelectorAll('.clickable-action[data-action-autostart="true"]:not([data-content-body])');

        autostart_actions.forEach(element => {
            element.click();
        });

        // Generate analytics events if tracking IDs are provided.

        const body = document.body;

        if (body.dataset.googleTrackingId && typeof gtag !== 'undefined') {
            gtag('set', {
                'custom_map': {
                    'dimension1': 'workshop_name',
                    'dimension2': 'session_name',
                    'dimension3': 'environment_name',
                    'dimension4': 'training_portal',
                    'dimension5': 'ingress_domain',
                    'dimension6': 'ingress_protocol',
                    'dimension7': 'session_owner'
                }
            });

            const gsettings = {
                'workshop_name': body.dataset.workshopName,
                'session_name': body.dataset.sessionNamespace,
                'environment_name': body.dataset.workshopNamespace,
                'training_portal': body.dataset.trainingPortal,
                'ingress_domain': body.dataset.ingressDomain,
                'ingress_protocol': body.dataset.ingressProtocol,
                'session_owner': dashboard.session_owner()
            };

            if (body.dataset.ingressProtocol === 'https') {
                gsettings['cookie_flags'] = 'max-age=86400;secure;samesite=none';
            }

            gtag('config', body.dataset.googleTrackingId, gsettings);
        }

        if (body.dataset.clarityTrackingId && typeof clarity !== 'undefined') {
            clarity('set', 'workshop_name', body.dataset.workshopName);
            clarity('set', 'session_name', body.dataset.sessionNamespace);
            clarity('set', 'environment_name', body.dataset.workshopNamespace);
            clarity('set', 'training_portal', body.dataset.trainingPortal);
            clarity('set', 'ingress_domain', body.dataset.ingressDomain);
            clarity('set', 'ingress_protocol', body.dataset.ingressProtocol);
            clarity('set', 'session_owner', dashboard.session_owner());
            clarity('identify', dashboard.session_owner());
        }

        if (body.dataset.amplitudeTrackingId && typeof amplitude !== 'undefined') {
            amplitude.init(body.dataset.amplitudeTrackingId, undefined, {
                defaultTracking: {
                    sessions: true,
                    pageViews: true,
                    formInteractions: true,
                    fileDownloads: true
                }
            });
        }

        if (!body.dataset.prevPage) {
            send_analytics_event('Workshop/First', {
                prev_page: body.dataset.prevPage,
                current_page: body.dataset.currentPage,
                next_page: body.dataset.nextPage,
                page_number: body.dataset.pageNumber,
                pages_total: body.dataset.pagesTotal,
            });
        }

        send_analytics_event('Workshop/View', {
            prev_page: body.dataset.prevPage,
            current_page: body.dataset.currentPage,
            next_page: body.dataset.nextPage,
            page_number: body.dataset.pageNumber,
            pages_total: body.dataset.pagesTotal,
        });

        if (!body.dataset.nextPage) {
            send_analytics_event('Workshop/Last', {
                prev_page: body.dataset.prevPage,
                current_page: body.dataset.currentPage,
                next_page: body.dataset.nextPage,
                page_number: body.dataset.pageNumber,
                pages_total: body.dataset.pagesTotal,
            });
        }
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

        if (original_glyph) {
            glyph_element.classList.add(original_glyph);
        } else {
            glyph_element.classList.add('fa-question-circle');
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

                    const original_glyph = element.dataset.originalGlyph;
                    if (original_glyph) {
                        glyph_element.classList.add(original_glyph);
                    } else {
                        glyph_element.classList.add('fa-question-circle');
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
            glyph_element.classList.remove('fa-spin', 'fa-cog', 'fa-check-circle', 'fa-times-circle', 'fa-question-circle');
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
                    element.dataset.originalGlyph = 'fa-check-circle';
                }
                break;

            case ActionState.FAILURE:
                element.dataset.actionResult = 'failure';
                element.dataset.actionCompleted = Date.now().toString();
                if (glyph_element) {
                    glyph_element.classList.remove('fa-cog', 'fa-spin');
                    element.dataset.originalGlyph = 'fa-times-circle';
                    glyph_element.classList.add('fa-times-circle');
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
    const ACTION_CASCADE_MS = 750;

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

        // If action is disabled, skip execution, but still trigger next
        // action in cascade if configured.

        const enabled = args.enabled !== undefined ? args.enabled : true;

        if (!enabled) {
            console.log(`Action ${action_id} is disabled`);
            if (args.cascade) {
                const pause = args.pause !== undefined ? args.pause * 1000 : ACTION_CASCADE_MS;
                setTimeout(() => trigger_next_action(element), pause);
            }
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

        // Send analytics event if configured for this action.

        if (args.event !== undefined) {
            const body = document.body;

            send_analytics_event('Action/Event', {
                prev_page: body.dataset.prevPage,
                current_page: body.dataset.currentPage,
                next_page: body.dataset.nextPage,
                page_number: body.dataset.pageNumber,
                pages_total: body.dataset.pagesTotal,
                event_name: args.event,
            });
        }

        // Set pending state.

        set_action_state(element, ActionState.PENDING);

        try {
            // Execute handler - await if it returns a promise.

            const result = handler_config.handler(element, args);

            if (result instanceof Promise) {
                // Apply timeout using Promise.race.

                const timeout = args.timeout ? args.timeout * 1000 : ACTION_TIMEOUT_MS;

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
                const pause = args.pause !== undefined ? args.pause * 1000 : ACTION_CASCADE_MS;
                setTimeout(() => trigger_next_action(element), pause);
            }

        } catch (error) {
            // Failure.

            set_action_state(element, ActionState.FAILURE, error);

            // Call finish callback on failure too.

            await call_finish_callback(ActionState.FAILURE, error);
        }
    }

    function trigger_next_action(element) {
        // Find the next action element by incrementing the numeric suffix in the ID.
        // IDs are of the form "clickable-action-nnn" where nnn is an integer.

        const current_id = element.id;
        const match = current_id.match(/^clickable-action-(\d+)$/);

        if (!match) {
            return;
        }

        const current_num = parseInt(match[1], 10);
        const next_id = `clickable-action-${current_num + 1}`;
        const next_action = document.getElementById(next_id);

        if (next_action && next_action.classList.contains('clickable-action')) {
            if (clickable_actions[next_id]) {
                execute_action(next_id);
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

        // If click is disabled on this element, skip default trigger behavior.
        // The action must be triggered explicitly by other means.

        if (element.dataset.clickDisabled === 'true') {
            return;
        }

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

            execute_in_terminal(command, session, clear);
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

            execute_in_all_terminals(command, clear);
        }
    });

    clickable_action_handler("terminal:interrupt", {
        handler: function (_element, args) {
            const defaults = {
                "session": "1",
            }

            args = { ...defaults, ...args }

            const session = args.session;

            interrupt_terminal(session);
        }
    });

    clickable_action_handler("terminal:interrupt-all", {
        handler: function (_element, _args) {
            interrupt_all_terminals();
        }
    });

    clickable_action_handler("terminal:clear", {
        handler: function (_element, args) {
            const defaults = {
                "session": "1",
            }

            args = { ...defaults, ...args }

            const session = args.session;

            clear_terminal(session);
        }
    });

    clickable_action_handler("terminal:clear-all", {
        handler: function (_element, _args) {
            clear_all_terminals();
        }
    });

    clickable_action_handler("terminal:input", {
        handler: function (_element, args) {
            const defaults = {
                "text": undefined,
                "session": "1",
                "endl": true,
            }

            args = { ...defaults, ...args }

            const text = args.text;
            const session = args.session || "1";
            const endl = args.endl;

            if (!text) {
                throw new Error("Text not provided");
            }

            if (endl) {
                paste_to_terminal(text + '\n', session);
            } else {
                paste_to_terminal(text, session);
            }
        }
    });

    clickable_action_handler("terminal:select", {
        handler: function (_element, args) {
            const defaults = {
                "session": "1"
            }

            args = { ...defaults, ...args }

            const session = args.session || "1";

            dashboard.expose_terminal(session);
        }
    });

    clickable_action_handler("workshop:copy", {
        handler: function (_element, args) {
            const defaults = {
                "text": undefined,
            }

            args = { ...defaults, ...args }

            const text = args.text;

            if (!text) {
                throw new Error("Text not provided");
            }

            set_paste_buffer_to_text(text);
        }
    });

    clickable_action_handler("dashboard:open-dashboard", {
        handler: function (_element, args) {
            const defaults = {
                "name": undefined,
            }

            args = { ...defaults, ...args }

            const name = args.name;

            if (!name) {
                throw new Error("Dashboard name not provided");
            }

            dashboard.expose_dashboard(name);
        }
    });

    clickable_action_handler("dashboard:create-dashboard", {
        handler: function (_element, args) {
            const defaults = {
                "name": undefined,
                "url": undefined,
                "focus": true
            }

            args = { ...defaults, ...args }

            const name = args.name;
            const url = args.url;
            const focus = args.focus;

            if (!name) {
                throw new Error("Dashboard name not provided");
            }

            if (!url) {
                throw new Error("Dashboard URL not provided");
            }

            dashboard.create_dashboard(name, url, focus);
        }
    });

    clickable_action_handler("dashboard:delete-dashboard", {
        handler: function (_element, args) {
            const defaults = {
                "name": undefined,
            }

            args = { ...defaults, ...args }

            const name = args.name;

            if (!name) {
                throw new Error("Dashboard name not provided");
            }

            dashboard.delete_dashboard(name);
        }
    });

    clickable_action_handler("dashboard:reload-dashboard", {
        handler: function (_element, args) {
            const defaults = {
                "name": undefined,
                "url": undefined,
                "focus": true
            }

            args = { ...defaults, ...args }

            const name = args.name;
            const url = args.url;
            const focus = args.focus;

            if (!name) {
                throw new Error("Dashboard name not provided");
            }

            dashboard.reload_dashboard(name, url, focus);
        }
    });

    clickable_action_handler("dashboard:open-url", {
        handler: function (_element, args) {
            const defaults = {
                "url": undefined,
            }

            args = { ...defaults, ...args }

            const url = args.url;

            if (!url) {
                throw new Error("URL not provided");
            }

            window.open(url, '_blank');
        }
    });

    clickable_action_handler("editor:open-file", {
        handler: function (_element, args) {
            const defaults = {
                "file": undefined,
                "line": 1
            };

            args = { ...defaults, ...args };

            if (!args.file) {
                throw new Error("File not provided");
            }

            dashboard.expose_dashboard("editor");

            return editor.open_file(args.file, args.line);
        }
    });

    clickable_action_handler("editor:select-matching-text", {
        handler: function (_element, args) {
            const defaults = {
                "file": undefined,
                "text": undefined,
                "start": undefined,
                "stop": undefined,
                "isRegex": false,
                "group": undefined,
                "before": undefined,
                "after": undefined
            };

            args = { ...defaults, ...args };

            if (!args.file) {
                throw new Error("File not provided");
            }

            if (!args.text) {
                throw new Error("Text to match not provided");
            }

            dashboard.expose_dashboard("editor");

            return editor.select_matching_text(
                args.file,
                args.text,
                args.start,
                args.stop,
                args.isRegex,
                args.group,
                args.before,
                args.after
            );
        }
    });

    clickable_action_handler("editor:replace-text-selection", {
        handler: function (_element, args) {
            const defaults = {
                "file": undefined,
                "text": undefined
            };

            args = { ...defaults, ...args };

            if (!args.file) {
                throw new Error("File not provided");
            }

            if (args.text === undefined) {
                throw new Error("Replacement text not provided");
            }

            dashboard.expose_dashboard("editor");

            return editor.replace_text_selection(args.file, args.text);
        }
    });

    clickable_action_handler("editor:append-lines-to-file", {
        handler: function (_element, args) {
            const defaults = {
                "file": undefined,
                "text": ""
            };

            args = { ...defaults, ...args };

            if (!args.file) {
                throw new Error("File not provided");
            }

            dashboard.expose_dashboard("editor");

            return editor.append_lines_to_file(args.file, args.text);
        }
    });

    clickable_action_handler("editor:insert-lines-before-line", {
        handler: function (_element, args) {
            const defaults = {
                "file": undefined,
                "line": undefined,
                "text": ""
            };

            args = { ...defaults, ...args };

            if (!args.file) {
                throw new Error("File not provided");
            }

            if (args.line === undefined) {
                throw new Error("Line number not provided");
            }

            dashboard.expose_dashboard("editor");

            return editor.insert_lines_before_line(args.file, args.line, args.text);
        }
    });

    clickable_action_handler("editor:append-lines-after-match", {
        handler: function (_element, args) {
            const defaults = {
                "file": undefined,
                "match": undefined,
                "text": ""
            };

            args = { ...defaults, ...args };

            if (!args.file) {
                throw new Error("File not provided");
            }

            if (!args.match) {
                throw new Error("Match string not provided");
            }

            dashboard.expose_dashboard("editor");

            return editor.append_lines_after_match(args.file, args.match, args.text);
        }
    });

    clickable_action_handler("editor:insert-value-into-yaml", {
        setup: function (element, args) {
            // Hugo can't display data as YAML directly, so we need to extract
            // out JSON from the code block located within the pre block with
            // class "clickable-action__body", reformat as YAML and insert it
            // back into the code block.

            const body_element = element.querySelector('.clickable-action__body code');

            if (body_element) {
                try {
                    const json_data = JSON.parse(body_element.textContent);
                    const yaml_data = jsyaml.dump(json_data);
                    body_element.textContent = yaml_data;
                } catch (error) {
                    console.error("Failed to convert JSON to YAML in action body:", error);
                }
            }
        },
        handler: function (_element, args) {
            const defaults = {
                "file": undefined,
                "path": undefined,
                "value": undefined
            };

            args = { ...defaults, ...args };

            if (!args.file) {
                throw new Error("File not provided");
            }

            if (!args.path) {
                throw new Error("YAML path not provided");
            }

            if (args.value === undefined) {
                throw new Error("Value not provided");
            }

            dashboard.expose_dashboard("editor");

            return editor.insert_value_into_yaml(args.file, args.path, args.value);
        }
    });

    clickable_action_handler("editor:execute-command", {
        handler: function (_element, args) {
            const defaults = {
                "command": undefined,
                "args": []
            };

            args = { ...defaults, ...args };

            if (!args.command) {
                throw new Error("Command not provided");
            }

            dashboard.expose_dashboard("editor");

            return editor.execute_command(args.command, args.args);
        }
    });

    clickable_action_handler("examiner:execute-test", {
        setup: function (element, args) {
            if (args.inputs && args.inputs.schema) {
                const header_element = element.querySelector('.clickable-action__header');
                const body_element = element.querySelector('.clickable-action__body');

                // Create form element.

                const form_element = document.createElement('form');

                // Configure form options with onSubmit callback.

                const form_options = {
                    ...args.inputs,
                    onSubmit: (errors, values) => {
                        if (!errors) {
                            execute_action(element.id);
                        }
                    }
                };

                // Initialize the form using jsonForm.

                $(form_element).jsonForm(form_options);

                // Create wrapper div with clickable-action__form class.

                const div_element = document.createElement('div');
                div_element.className = 'clickable-action__form';
                div_element.prepend(form_element);

                // Prevent Enter key from submitting in non-textarea inputs.

                form_element.addEventListener('keydown', function (event) {
                    if (event.target.tagName !== 'TEXTAREA' && event.key === 'Enter') {
                        event.preventDefault();
                    }
                });

                // Insert form div after the header element.

                header_element.after(div_element);

                // Hide the body element.

                body_element.style.display = 'none';

                // Disable default click-to-trigger so only submit button triggers action.

                element.dataset.clickDisabled = 'true';
            }
        },
        handler: function (element, args) {
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

            // Process form if it exists.

            let form_values = {};
            let form_object = element.querySelector('.clickable-action__form > form');

            if (form_object) {
                let form_data = new FormData(form_object);
                let object = {};
                form_data.forEach((value, key) => {
                    if (!Reflect.has(object, key)) {
                        object[key] = value;
                        return;
                    }
                    if (!Array.isArray(object[key])) {
                        object[key] = [object[key]];
                    }
                    object[key].push(value);
                });
                form_values = object;
            }

            return examiner.execute_test(args.name, {
                args: args.args,
                form: form_values,
                timeout: args.timeout,
                retries: args.retries,
                delay: args.delay
            });
        }
    });

    clickable_action_handler("files:download-file", {
        setup: function (element, args) {
            // If preview is enabled, fetch and display file content in the body.

            if (args.preview) {
                const body_element = element.querySelector('.clickable-action__body code');

                if (body_element) {
                    let url = `/files/${args.path}`;

                    if (args.url) {
                        url = args.url;
                    }

                    fetch(url)
                        .then(response => response.text())
                        .then(text => {
                            body_element.textContent = text;
                        })
                        .catch(error => console.error('Failed to fetch file preview:', error));
                }
            }
        },
        handler: function (_element, args) {
            const defaults = {
                "path": undefined,
                "url": undefined,
                "download": undefined,
                "preview": false
            };

            args = { ...defaults, ...args };

            if (args.url) {
                return fetch(args.url)
                    .then(response => response.text())
                    .then(text => {
                        const url = new URL(args.url);
                        const pathname = url.pathname;
                        const basename = pathname.split('/').pop() || url.hostname || 'download.txt';

                        const download_link = document.createElement('a');
                        const blob = new Blob([text], { type: 'octet/stream' });

                        download_link.setAttribute('href', window.URL.createObjectURL(blob));
                        download_link.setAttribute('download', args.download || basename);
                        download_link.style.display = 'none';

                        document.body.appendChild(download_link);
                        download_link.click();
                        document.body.removeChild(download_link);
                    });
            } else {
                const pathname = `/files/${args.path}`;
                const basename = pathname.split('/').pop();

                const download_link = document.createElement('a');

                download_link.setAttribute('href', pathname);
                download_link.setAttribute('download', args.download || basename);
                download_link.style.display = 'none';

                document.body.appendChild(download_link);
                download_link.click();
                document.body.removeChild(download_link);

                return Promise.resolve();
            }
        }
    });

    clickable_action_handler("files:copy-file", {
        setup: function (element, args) {
            // If preview is enabled, fetch and display file content in the body.
            // The content is also cached for use by the handler.

            if (args.preview) {
                let url = `/files/${args.path}`;

                if (args.url) {
                    url = args.url;
                }

                fetch(url)
                    .then(response => response.text())
                    .then(text => {
                        // Cache the content for use in handler.

                        element.dataset.cachedFileContent = text;

                        // Display in the body.

                        const body_element = element.querySelector('.clickable-action__body code');

                        if (body_element) {
                            body_element.textContent = text;
                        }
                    })
                    .catch(error => console.error('Failed to fetch file preview:', error));
            }
        },
        handler: function (element, args) {
            // Use cached content if available (from preview fetch).

            const cached_content = element.dataset.cachedFileContent;

            if (cached_content !== undefined) {
                set_paste_buffer_to_text(cached_content);
                return Promise.resolve();
            }

            // Fetch content if not cached. The fallback in set_paste_buffer_to_text
            // using execCommand should handle cases where the Clipboard API fails
            // due to user activation expiry.

            const defaults = {
                "path": undefined,
                "url": undefined
            };

            args = { ...defaults, ...args };

            let url = `/files/${args.path}`;

            if (args.url) {
                url = args.url;
            }

            return fetch(url)
                .then(response => response.text())
                .then(text => {
                    set_paste_buffer_to_text(text);
                });
        }
    });

    clickable_action_handler("files:upload-file", {
        setup: function (element, args) {
            const header_element = element.querySelector('.clickable-action__header');
            const body_element = element.querySelector('.clickable-action__body');

            // Create form element with file input.

            const form_element = document.createElement('form');

            form_element.innerHTML = `
                <div class="form-group">
                    <input type="hidden" name="path" value="${args.path || ''}">
                    <input type="file" class="form-control-file" name="file" id="file" required>
                </div>
                <div class="form-group my-2">
                    <input type="submit" class="btn btn-primary" id="form-action-submit" value="Upload">
                </div>
            `;

            // Create wrapper div with clickable-action__form class.

            const div_element = document.createElement('div');

            div_element.className = 'clickable-action__form';
            div_element.appendChild(form_element);

            // Prevent Enter key from submitting in non-textarea inputs.

            form_element.addEventListener('keydown', function (event) {
                if (event.target.tagName !== 'TEXTAREA' && event.key === 'Enter') {
                    event.preventDefault();
                }
            });

            // Handle form submission.

            form_element.addEventListener('submit', function (event) {
                event.preventDefault();

                if (form_element.checkValidity()) {
                    execute_action(element.id);
                } else {
                    form_element.reportValidity();
                }
            });

            // Insert form div after the header element.

            header_element.after(div_element);

            // Hide the body element.

            body_element.style.display = 'none';

            // Disable default click-to-trigger so only submit button triggers action.

            element.dataset.clickDisabled = 'true';
        },
        handler: function (element, args) {
            const form_element = element.querySelector('.clickable-action__form > form');

            if (!form_element) {
                return Promise.reject(new Error('Form not found'));
            }

            const form_data = new FormData(form_element);

            return fetch('/upload/file', {
                method: 'POST',
                body: form_data
            })
                .then(response => {
                    if (response.status !== 200) {
                        throw new Error('Upload failed');
                    }
                    return response.text();
                })
                .then(data => {
                    if (data !== 'OK') {
                        throw new Error('Upload failed');
                    }
                });
        }
    });

    clickable_action_handler("files:upload-files", {
        setup: function (element, args) {
            const header_element = element.querySelector('.clickable-action__header');
            const body_element = element.querySelector('.clickable-action__body');

            // Create form element with multiple file input.

            const form_element = document.createElement('form');

            form_element.innerHTML = `
                <div class="form-group">
                    <input type="hidden" name="directory" value="${args.directory || ''}">
                    <input type="file" class="form-control-file" name="files" id="files" multiple required>
                </div>
                <div class="form-group my-2">
                    <input type="submit" class="btn btn-primary" id="form-action-submit" value="Upload">
                </div>
            `;

            // Create wrapper div with clickable-action__form class.

            const div_element = document.createElement('div');

            div_element.className = 'clickable-action__form';
            div_element.appendChild(form_element);

            // Prevent Enter key from submitting in non-textarea inputs.

            form_element.addEventListener('keydown', function (event) {
                if (event.target.tagName !== 'TEXTAREA' && event.key === 'Enter') {
                    event.preventDefault();
                }
            });

            // Handle form submission.

            form_element.addEventListener('submit', function (event) {
                event.preventDefault();

                if (form_element.checkValidity()) {
                    execute_action(element.id);
                } else {
                    form_element.reportValidity();
                }
            });

            // Insert form div after the header element.

            header_element.after(div_element);

            // Hide the body element.

            body_element.style.display = 'none';

            // Disable default click-to-trigger so only submit button triggers action.

            element.dataset.clickDisabled = 'true';
        },
        handler: function (element, args) {
            const form_element = element.querySelector('.clickable-action__form > form');

            if (!form_element) {
                return Promise.reject(new Error('Form not found'));
            }

            const form_data = new FormData(form_element);

            return fetch('/upload/files', {
                method: 'POST',
                body: form_data
            })
                .then(response => {
                    if (response.status !== 200) {
                        throw new Error('Upload failed');
                    }
                    return response.text();
                })
                .then(data => {
                    if (data !== 'OK') {
                        throw new Error('Upload failed');
                    }
                });
        }
    });

    clickable_action_handler("section:heading", {
        handler: function (_element, args) { }
    });

    clickable_action_handler("section:begin", {
        setup: function (element, args) {
            const name = args.name || '';

            element.dataset.sectionName = name;
            element.dataset.contentState = 'hidden';
        },
        handler: function (element, args) {
            const name = args.name || '';

            // Collect following elements up to (but not including) the matching
            // section:end.

            const following_elements = [];
            let section_end_element = null;
            let sibling = element.nextElementSibling;

            while (sibling) {
                // Check if this is the matching section:end.

                if (sibling.classList.contains('clickable-action') &&
                    sibling.dataset.handler === 'section:end' &&
                    sibling.dataset.sectionName === name) {
                    section_end_element = sibling;
                    break;
                }

                following_elements.push(sibling);
                sibling = sibling.nextElementSibling;
            }

            // Filter to elements with matching contentBody.

            const content_elements = following_elements.filter(
                el => el.dataset.contentBody === name
            );

            if (element.dataset.contentState === 'hidden') {
                // Reveal elements with matching contentBody, but don't change
                // contentState for nested section:begin or section:end elements.

                content_elements.forEach(el => {
                    const is_section_element = el.classList.contains('clickable-action') &&
                        (el.dataset.handler === 'section:begin' || el.dataset.handler === 'section:end');

                    if (!is_section_element) {
                        el.dataset.contentState = 'visible';
                    }

                    el.style.display = '';
                });

                element.dataset.contentState = 'visible';

                // Trigger autostart clickable actions within the revealed section.

                content_elements.forEach(el => {
                    if (el.classList.contains('clickable-action') &&
                        el.dataset.actionAutostart === 'true') {
                        execute_action(el.id);
                    }
                });
            } else {
                // Hide all elements between section:begin and section:end.

                following_elements.forEach(el => {
                    el.dataset.contentState = 'hidden';
                    el.style.display = 'none';
                });

                if (section_end_element) {
                    section_end_element.dataset.contentState = 'hidden';
                }

                element.dataset.contentState = 'hidden';
            }
        },
        finish: function (element, args, state, _error) {
            if (state === 'success') {
                if (element.dataset.contentState === 'hidden') {
                    // Override icon to fa-chevron-down.
                    const glyph_element = element.querySelector('.clickable-action__icon');
                    if (glyph_element) {
                        glyph_element.classList.remove('fa-check-circle', 'fa-chevron-up');
                        element.dataset.originalGlyph = 'fa-chevron-down';
                        glyph_element.classList.add('fa-chevron-down');
                    }
                } else {
                    // Override icon to fa-chevron-up.
                    const glyph_element = element.querySelector('.clickable-action__icon');
                    if (glyph_element) {
                        glyph_element.classList.remove('fa-check-circle', 'fa-chevron-down');
                        element.dataset.originalGlyph = 'fa-chevron-up';
                        glyph_element.classList.add('fa-chevron-up');
                    }
                }
            }
        }
    });

    clickable_action_handler("section:end", {
        setup: function (element, args) {
            const name = args.name || '';

            element.dataset.sectionName = name;
            element.dataset.contentBody = name;

            element.dataset.contentState = 'hidden';
            element.style.display = 'none';

            // Gather all preceding elements up to (but not including) the
            // matching section:begin.

            const preceding_elements = [];
            let sibling = element.previousElementSibling;

            while (sibling) {
                // Check if this is the matching section:begin.

                if (sibling.classList.contains('clickable-action') &&
                    sibling.dataset.handler === 'section:begin' &&
                    sibling.dataset.sectionName === name) {
                    break;
                }

                preceding_elements.push(sibling);
                sibling = sibling.previousElementSibling;
            }

            // Filter out elements that already have contentBody set, then set
            // it and hide it.

            preceding_elements
                .filter(el => !el.dataset.contentBody)
                .forEach(el => {
                    el.dataset.contentBody = name;
                    el.dataset.contentState = 'hidden';
                    el.style.display = 'none';
                });
        },
        handler: function (element, args) {
            const name = args.name || '';

            // Find the matching section:begin element and trigger click on it.

            let sibling = element.previousElementSibling;

            while (sibling) {
                if (sibling.classList.contains('clickable-action') &&
                    sibling.dataset.handler === 'section:begin' &&
                    sibling.dataset.sectionName === name) {
                    delete sibling.dataset.actionCompleted;
                    sibling.click();
                    break;
                }

                sibling = sibling.previousElementSibling;
            }
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
