# Ledgerly

Small invoicing service. All runtime settings live in `config/app.json`;
`src/settings.py` loads that file at startup and rejects obviously bad values.

To check the config after editing it:

    python3 src/settings.py
