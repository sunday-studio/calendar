-- all-day occurrences are windowed by date, not instant
CREATE INDEX occ_allday ON occurrences(start_date) WHERE all_day = 1;
