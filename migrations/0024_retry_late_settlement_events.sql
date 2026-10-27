-- Events refused because a player had settled the planet past their due time
-- were dropped after their last attempt, leaving a construction or a returning
-- fleet stuck for good. They can apply now; give them their attempts back.
UPDATE scheduled_events
SET attempts = 0, last_error = NULL
WHERE state = 'pending' AND last_error = 'economy: production time cannot move backwards';
