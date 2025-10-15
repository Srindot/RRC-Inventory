import segno

MOCHA_BASE = '#1e1e2e'    
MOCHA_TEXT = '#cdd6f4'    
MOCHA_FLAMINGO = '#f2cdcd'

qrcode = segno.make_qr("google.com", error='h')

qrcode.save(
    "catppuccin_mocha_qrcode.png",
    scale=10, 
    

    light=MOCHA_BASE,
    

    dark=MOCHA_TEXT,
    

    data_dark=MOCHA_FLAMINGO,
)

print("generated")