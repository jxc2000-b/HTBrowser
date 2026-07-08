{
  "cycle": {
    "id": 1313520632,
    "created_at": "2026-02-16T15:07:42.694+0000",
    "updated_at": "2026-02-17T15:23:23.174+0000",
    "user_id": YOUR_USER_ID,
    "during": "['2026-02-15T06:57:35.430Z','2026-02-16T07:09:07.160Z')",
    "days": "['2026-02-15','2026-02-16')",
    "timezone_offset": "-0800",
    "data_state": "complete",
    "scaled_strain": 13.400938,
    "day_strain": 0.00761,
    "day_kilojoules": 10370.79,
    "day_avg_heart_rate": 65,
    "day_max_heart_rate": 187,
    "sleep_need": null,
    "predicted_end": "2026-02-16T07:09:07.160+0000",
    "intensity_score": null
  },

  "sleeps": [
    {
      "activity_id": "some-uuid",
      "activity_version": 0,
      "user_id": YOUR_USER_ID,
      "created_at": "2026-02-16T15:07:42.416Z",
      "updated_at": "2026-02-16T15:07:42.416Z",
      "during": "['2026-02-15T06:57:35.430Z','2026-02-15T14:59:35.310Z')",
      "timezone_offset": "-08:00",
      "state": "complete",
      "source": "auto+user",
      "survey_response_id": null,
      "is_nap": false,
      "normal": true,
      "significant": true,
      "score": 92,
      "projected_score": 92.0,
      "sleep_consistency": 84.0,

      // Durations — all in MILLISECONDS
      "quality_duration": 27928890,
      "time_in_bed": 29233940.0,
      "light_sleep_duration": 12563150,
      "slow_wave_sleep_duration": 6512940,
      "rem_sleep_duration": 8852800,
      "wake_duration": 1305100,
      "arousal_time": 600000.0,
      "no_data_duration": 0,
      "latency": 0,
      "projected_sleep": 27928890.0,
      "credit_from_naps": 0.0,

      // Sleep debt — in MILLISECONDS
      "sleep_need": 30004136.0,
      "habitual_sleep_need": 28053705.0,
      "need_from_strain": 1950431.0,
      "debt_pre": 2075712.0,
      "debt_post": 5006822.0,

      // Vitals
      "respiratory_rate": 13.828125,
      "in_sleep_efficiency": 0.95037776,

      // Sleep cycles & disruptions
      "cycles_count": 7,
      "disturbances": 8,
      "total_wake_events": 11,

      // Optimal window (PostgreSQL range string)
      "optimal_sleep_times": "['2026-02-15T06:15:00.000Z','2026-02-15T14:55:00.000Z')",

      "algo_version": "9.1.8.2"
    }
  ],

  "recovery": {
    "activity_id": "some-uuid",
    "activity_version": 0,
    "user_id": YOUR_USER_ID,
    "created_at": "2026-02-16T15:07:42.416Z",
    "updated_at": "2026-02-16T15:07:42.416Z",
    "state": "complete",
    "responded": false,
    "survey_response_id": null,
    "calibrating": false,
    "normal": true,
    "recovery_score": 95,
    "resting_heart_rate": 46,
    "hrv_rmssd": 0.099190265,
    "hr_baseline": 48.0,
    "skin_temp_celsius": 33.199333,
    "spo2": 97.5,
    "prob_covid": 0.104,
    "rhr_component": 0.7980845,
    "hrv_component": 0.93978375,
    "history_size": 8.0,
    "recovery_rate": null,
    "algo_version": "mav.8.3.0"
  },

  "workouts": [
    {
      "activity_id": "d9df8189-fd7a-425b-a47c-466d6c7fdf31",
      "activity_version": 2,
      "during": "['2026-02-16T01:09:00.000Z','2026-02-16T02:05:59.720Z')",
      "timezone_offset": "-08:00",
      "source": "user",
      "survey_response_id": null,
      "sport_id": 45,

      "score": 12.696353,
      "intensity_score": 12.696353,
      "cumulative_workout_intensity": 15.182196,
      "raw_intensity_score": 0.00638894,
      "percent_recorded": 0.99978,
      "kilojoules": 1187.046,
      "max_heart_rate": 161,
      "average_heart_rate": 108,
      "total_steps": null,
      "gps_data": null,

      // Heart rate zones — durations in SECONDS (% of max HR)
      "zone_durations_v2": {
        "zone0_to50_duration": 1214.02,
        "zone50_to60_duration": 1829.96,
        "zone60_to70_duration": 284.0,
        "zone70_to80_duration": 91.0,
        "zone80_to90_duration": 0.0,
        "zone90_to100_duration": 0.0
      },
      // Legacy zone array: [zone0-50, zone50-60, zone60-70, zone70-80, zone80-90, zone90-100]
      "zone_durations": [1214.02, 1829.96, 284.0, 91.0, 0.0, 0.0],

      // MSK (musculoskeletal) score — present for strength activities, null for cardio-only
      "msk_score": {
        "raw_cardio_intensity_score": 0.001739,
        "scaled_cardio_intensity_score": 7.094,
        "raw_msk_intensity_score": 0.159138,
        "scaled_msk_intensity_score": 11.148,
        "cardio_strain_contribution_percent": 0.27226
      }
    }
  ],

  "v2_activities": [
    {
      "id": "d9df8189-fd7a-425b-a47c-466d6c7fdf31",
      "cycle_id": 1313520632,
      "user_id": YOUR_USER_ID,
      "created_at": "2026-02-16T02:21:16.167+0000",
      "updated_at": "2026-02-16T04:12:33.681+0000",
      "version": 2,
      "during": "['2026-02-16T01:09:00.000Z','2026-02-16T02:05:59.720Z')",
      "timezone": "America/Los_Angeles",
      "timezone_offset": null,
      "timezone_offset_from_model": "-08:00",
      "source": "user",
      "source_id": null,
      "activity_v1_id": null,
      "score_state": "complete",
      "score_type": "CARDIO",
      "type": "weightlifting",
      "translated_type": "Weightlifting"
    }
  ]
}