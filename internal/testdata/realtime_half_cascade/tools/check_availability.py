_TIMES = ("09:00", "11:30", "15:00")


def check_availability(service, date):
    # ponytail: mock tool, no validation — always offers the three fixed slots
    return {
        "slots": [
            {
                "slot_id": f"{date}_{service}_{time.replace(':', '')}",
                "start_time": f"{date}T{time}:00-05:00",
            }
            for time in _TIMES
        ]
    }
