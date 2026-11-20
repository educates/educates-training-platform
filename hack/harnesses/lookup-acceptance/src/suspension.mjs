// Whether the harness was suspended during a run, as when the host sleeps.
// A sleep stops the clusters' clocks too, and when the host wakes they jump
// forward: the portals then expire the run's sessions, which were meant to
// last the whole run, and every check after the sleep sees what the jump
// did rather than what the Lookup Service did. A run with a suspension
// proves nothing either way.

// A timer the harness sets ticks every few seconds while it runs. A gap
// longer than this between two ticks is a suspension: a busy host delays a
// timer by seconds, never by a minute.
const THRESHOLD_MS = 60_000;
const INTERVAL_MS = 5_000;

export class SuspensionWatch {
  constructor({ onSuspension } = {}) {
    this.onSuspension = onSuspension;
    this.suspensions = [];
    this.last = null;
    this.timer = null;
  }

  // note records that the harness ran at now, a time in milliseconds, and
  // returns the suspension that ended then, if there was one.
  note(now) {
    const last = this.last;
    this.last = now;
    if (last === null || now - last <= THRESHOLD_MS) return undefined;
    const suspension = {
      from: new Date(last).toISOString(),
      to: new Date(now).toISOString(),
      seconds: Math.round((now - last) / 1000),
    };
    this.suspensions.push(suspension);
    this.onSuspension?.(suspension);
    return suspension;
  }

  start() {
    this.note(Date.now());
    this.timer = setInterval(() => this.note(Date.now()), INTERVAL_MS);
    this.timer.unref();
  }

  stop() {
    clearInterval(this.timer);
    this.note(Date.now());
  }
}
