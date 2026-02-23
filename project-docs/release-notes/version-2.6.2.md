---
orphan: true
---

Version 2.6.2
=============

Bugs Fixed
----------

* The expiry time for refresh tokens was set to only 1 hour, meaning that if a
  workshop session had a duration longer than 10 hours (expiry time of access
  token), if you didn't visit the dashboard for the workshop session in the time
  period from 15 minutes before the 10 hour point and 1-2 hour after that point,
  the refresh token would have expired and the access token couldn't be
  refreshed. This would result in the countdown clock not working and it would
  not be displayed. It also removed the ability to extend the workshop session.
  A fix for this was needed on top of changes already made in 2.5.2 to ensure an
  attempt was made to refresh the access token. The change made was to set the
  expiry time for the refresh token to 30 days instead of 1 hour.
