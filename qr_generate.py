import segno

# Create the QR code data
qrcode = segno.make_qr("http://10.2.36.243", error='h')

# Save the QR code as a black and white PNG file
qrcode.save(
    "black_and_white_qrcode.png",
    scale=10,
    light='white',  # Sets the background color to white
    dark='black'    # Sets the QR code modules to black
)

print("Generated black and white QR code.")