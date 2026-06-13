# Wearable Export

debug dump from the ring app, weekly aggregates. epoch ts = week start. units are a mess, sorry: sleep is in MINUTES here, the app displays hours.

week_start=1767571200 (2026-01-05) sleep_total_min=432 deep_min=66 rem_min=98 rhr_bpm=61 hrv_ms=44 steps=51203
week_start=1768176000 (2026-01-12) sleep_total_min=405 deep_min=58 rem_min=91 rhr_bpm=63 hrv_ms=41 steps=44890
week_start=1768780800 (2026-01-19) sleep_total_min=448 deep_min=71 rem_min=103 rhr_bpm=60 hrv_ms=47 steps=58117
week_start=1769385600 (2026-01-26) sleep_total_min=391 deep_min=49 rem_min=84 rhr_bpm=64 hrv_ms=38 steps=39455

app also exported this json blob for "monthly summary":

{"month":"2026-01","sleep_avg_h":6.99,"score":{"sleep":78,"readiness":81,"activity":74},"goal_adherence_pct":67.5,"best_night":{"date":"2026-01-21","score":91}}

NB the 6.99h avg in the json is the app's own conversion of the minute values above. spo2 sensor was disabled all month, no data.
