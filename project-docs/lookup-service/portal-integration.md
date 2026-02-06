(portal-integration)=
Portal Integration
==================

When using the lookup service REST API to provide access to workshops from a custom front-end portal, there are several integration considerations to be aware of, particularly around how users are redirected when a workshop session ends and how workshop sessions can be embedded within a larger web application.

Accessing a workshop session
----------------------------

After [requesting a workshop session](requesting-a-workshop-session) through the REST API, the response includes a ``sessionActivationUrl``. The custom portal needs to direct the end user's browser to this URL to activate and access the workshop session.

There are two approaches:

* **New browser window or tab** - Open the activation URL in a new browser window or tab. This is the simplest approach and avoids complications with content security policies and frame embedding.

* **Embedded iframe** - Load the activation URL in an iframe within the custom portal's page. This provides a more integrated experience but requires additional configuration as described below.

The activation URL contains a token that is valid for 60 seconds. The user's browser must reach this URL within that window or the session will be cleaned up and a new request will need to be made.

Browser redirection on session end
----------------------------------

When a workshop session ends, the user's browser is redirected to a return URL. This happens when:

* The user completes all workshop instructions and clicks "Finish Workshop".
* The user selects "Terminate Session" from the workshop menu.
* The user clicks the exit icon in the workshop dashboard.
* The workshop duration expires (a countdown popup is shown before expiry).

If a ``clientIndexUrl`` was provided in the session request, the user will be redirected to that URL when the session ends. If ``clientIndexUrl`` was not provided, the user will be redirected to the training portal's own index page.

When the user is redirected back to the ``clientIndexUrl``, a ``notification`` query string parameter will be appended to indicate the reason for the redirect. The possible values are:

* ``session-deleted`` - The workshop session was completed or terminated.
* ``workshop-invalid`` - The workshop name was invalid.
* ``session-unavailable`` - No capacity was available for the workshop.
* ``session-invalid`` - The session no longer exists (e.g., it expired and was cleaned up).
* ``startup-timeout`` - The workshop session did not start within the required time.

The custom portal can inspect this parameter to display an appropriate message to the user or to take other actions such as refreshing the workshop catalog.

Embedding in an iframe
----------------------

Workshop sessions can be embedded in an iframe within the custom portal's page. To do this, direct the iframe's ``src`` to the ``sessionActivationUrl``:

```html
<iframe src="<sessionActivationUrl>" width="100%" height="800px"></iframe>
```

When embedding in an iframe, there are additional considerations:

**Content Security Policy (CSP):** The Educates training portal must be configured to allow its content to be embedded by the custom portal's domain. If the CSP headers are not configured to permit framing by the custom portal's origin, the browser will block the embedded content. This requires configuration on the Educates deployment side, not just in the custom portal.

**Redirection behavior:** When a workshop session ends and the user is redirected to the ``clientIndexUrl``, the redirect will occur within the iframe rather than in the top-level browser window. This means the custom portal page would be loaded inside the iframe, resulting in undesirable nesting.

Handling iframe redirection
---------------------------

To avoid nested pages when using iframe embedding, the workshop dashboard attempts to use JavaScript to redirect the top-level browser window (via ``window.top.location``) rather than just the iframe. However, this JavaScript-based redirect can be blocked by the browser's content security policy when cross-origin frames are involved.

There are several strategies for handling this:

**JavaScript redirect handler:** Instead of pointing ``clientIndexUrl`` directly at the custom portal's main page, point it at a lightweight HTML page hosted on the same domain as the custom portal that uses JavaScript to redirect the top-level window:

```
clientIndexUrl=https://portal.example.com/redirect?target=https://portal.example.com/workshops
```

The redirect handler page would use JavaScript such as ``window.top.location.href`` or ``window.parent.location.href`` to navigate the full browser window to the desired destination.

**Close browser tab:** Another approach is to set the ``clientIndexUrl`` to a page that attempts to close the browser tab or window. This works well when the workshop was opened in a new tab rather than an iframe:

```
clientIndexUrl=https://portal.example.com/close
```

The close page would use ``window.close()`` and display a fallback message asking the user to close the tab manually if the browser blocks the automatic close.

**Fallback behavior:** If the JavaScript redirect is blocked by the browser's security policies, the user will see the target page rendered within the iframe. The custom portal should be prepared for this case and may want to display a message instructing the user to return to the main portal page.

The best approach depends on the specific deployment configuration and whether CSP headers can be adjusted. Testing with the actual production CSP settings is recommended to verify the chosen redirect strategy works correctly.
