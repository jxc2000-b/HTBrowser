{
  "device": {
    "manufacturer": "ExampleWatch",
    "model": "Pulse 4",
    "firmware": "8.2.1",
    "serial": "redacted"
  },
  "user_profile": {
    "height_cm": 178,
    "weight_kg": 76,
    "sex": "unspecified",
    "timezone": "America/New_York"
  },
  "samples": [
    {
      "timestamp": "2026-07-08T07:12:00-04:00",
      "type": "heart_rate",
      "value": 62,
      "unit": "bpm",
      "source": "optical_sensor"
    },
    {
      "timestamp": "2026-07-08T07:12:00-04:00",
      "type": "steps",
      "value": 14,
      "unit": "count"
    },
    {
      "timestamp": "2026-07-08T07:12:00-04:00",
      "type": "blood_oxygen",
      "value": 97,
      "unit": "%"
    },
    {
      "timestamp": "2026-07-08T07:12:00-04:00",
      "type": "skin_temperature",
      "value": 33.8,
      "unit": "C"
    }
  ],
  "activity_sessions": [
    {
      "id": "workout_20260708_0615",
      "type": "run",
      "start_time": "2026-07-08T06:15:03-04:00",
      "end_time": "2026-07-08T06:47:28-04:00",
      "duration_sec": 1945,
      "distance_m": 5120,
      "active_calories_kcal": 382,
      "avg_heart_rate_bpm": 148,
      "max_heart_rate_bpm": 171,
      "gps_track": [
        {
          "timestamp": "2026-07-08T06:15:10-04:00",
          "lat": 40.7128,
          "lon": -74.0060,
          "elevation_m": 12.4
        }
      ]
    }
  ],
  "sleep": [
    {
      "date": "2026-07-07",
      "start_time": "2026-07-07T23:18:00-04:00",
      "end_time": "2026-07-08T06:42:00-04:00",
      "total_sleep_min": 424,
      "stages": [
        {
          "stage": "light",
          "start": "2026-07-07T23:18:00-04:00",
          "end": "2026-07-08T00:04:00-04:00"
        },
        {
          "stage": "deep",
          "start": "2026-07-08T00:04:00-04:00",
          "end": "2026-07-08T00:46:00-04:00"
        },
        {
          "stage": "rem",
          "start": "2026-07-08T04:12:00-04:00",
          "end": "2026-07-08T04:38:00-04:00"
        }
      ]
    }
  ]
}