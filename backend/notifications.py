"""Notification enqueue policy.

Page delivery is represented in the snapshot/outbox. SMS remains explicitly disabled
until a real provider worker and delivery receipt flow are implemented.
"""


class FakeNotificationService:
    def enqueue(self, db, event):
        cycle = event["notification"]["cycle"]
        for channel, status in (("page", "available"), ("sms", "disabled")):
            db.execute(
                "INSERT OR IGNORE INTO notification_outbox(event_id,cycle,channel,status) VALUES(?,?,?,?)",
                (event["id"], cycle, channel, status),
            )
