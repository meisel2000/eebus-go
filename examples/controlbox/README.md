
# EVCC + Controlbox-Simulator Configuration

## Add EEBus configuration to EVCC 
Create EEBus cert for evcc

```
evcc eebus-cert
[main  ] INFO 2025/02/07 20:13:11 evcc 0.132.1

Add the following to the evcc config file:

eebus:
  certificate:
    public: |
      -----BEGIN CERTIFICATE-----
      MIIBvTCCAWOgAwIBAgIRAKY6cWkJteXQqgLp4
      ...
      M8YGKSd8dlVtyZQu1vM7VmI=
      -----END CERTIFICATE-----
      
    private: |
      -----BEGIN EC PRIVATE KEY-----
      MHcCAQEEIByJ00M/FMKBrVH8MnCwEXS/
      ...
      -----END EC PRIVATE KEY-----
      
```

## Controlbox configuration
Generate eebus.crt + eebus.key
Store certificate from output in eebus.crt file
Store private key from output in eebus.key file
Grep local SKI from output

Example:
```
go run main.go 8181 

-----BEGIN CERTIFICATE-----
MIIBxjCCAWugAwIBAgIRA2NclvXFEqMvKE/KA28Ile0wCgYIKoZIzj0EAwIwQjEL
...
Hh6SOdAT67JcsBfH10lpEc0kG4zWlxF/d5Q=
-----END CERTIFICATE-----

-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIIskfvH2vllGa/EIWphJ
...
RkpQ7/vTklMxk+sYzXwRzw==
-----END EC PRIVATE KEY-----

2025-02-07 20:41:54 INFO  Local SKI: c6bdd44deab084c9e73e1eceecbea33425ed3b7d
```

# Start evcc

Add hems with SKI of controllbox to evcc.yaml
```
hems:
  type: eebus
  ski: <ski of controlbox>
```

Example:
```
hems:
  type: eebus
  # ski: <ski of controlbox>
  ski: c6bdd44deab084c9e73e1eceecbea33425ed3b7d

```

Restart evcc and grep log for local ski
```
[eebus ] INFO 2025/02/07 20:05:56 Local SKI: 1e238613c409407420e5bfa97955309eeb62876c
```

# Start Controlbox simulator

Start controlbox simulator
`go run main <port> <evcc ski> <crtfile> <keyfile>`

Example:
```
go run main.go 8181 1e238613c409407420e5bfa97955309eeb62876c eebus.crt eebus.key
```

# Install frontend

```
npm install
```

# Start Controlbox frontend 
```
npm run dev
```

