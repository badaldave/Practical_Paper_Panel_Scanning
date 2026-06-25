import os

xls_path = "/var/data/uploads/SCC58.XLS"
if os.path.exists(xls_path):
    with open(xls_path, 'rb') as f:
        head = f.read(500)
        print("Header Bytes:", head[:50])
        try:
            print("As text (UTF-8):", head.decode('utf-8'))
        except Exception as e:
            try:
                print("As text (latin-1):", head.decode('latin-1'))
            except Exception as e2:
                print("Cannot decode as text:", e2)
