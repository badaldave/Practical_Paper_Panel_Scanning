import struct

def read_dbf(file_path):
    with open(file_path, 'rb') as f:
        # 1. Read main header
        header = f.read(32)
        if len(header) < 32:
            return []
        version, yy, mm, dd, num_records, header_len, record_len = struct.unpack('<BBBBLHH', header[:12])
        
        # 2. Read field descriptors
        fields = []
        num_fields = (header_len - 33) // 32
        for _ in range(num_fields):
            field_data = f.read(32)
            if len(field_data) < 32:
                break
            name = field_data[:11].strip(b'\x00').decode('latin-1').strip()
            field_type = chr(field_data[11])
            field_len = field_data[16]
            fields.append((name, field_type, field_len))
            
        # Skip the terminator byte (usually 0x0D)
        f.seek(header_len)
        
        # 3. Read records
        records = []
        for _ in range(num_records):
            record_data = f.read(record_len)
            if len(record_data) < record_len:
                break
            # First byte is delete flag (0x20 is active, 0x2A is deleted)
            if record_data[0] == 0x2A:
                continue
                
            record = {}
            offset = 1
            for name, field_type, field_len in fields:
                val = record_data[offset:offset+field_len].decode('latin-1').strip()
                record[name] = val
                offset += field_len
            records.append(record)
            
        return fields, records

if __name__ == '__main__':
    fields, records = read_dbf("/var/data/uploads/SCC58.XLS")
    print("Fields:", fields)
    print("Number of records:", len(records))
    print("First 10 records:")
    for r in records[:10]:
        print(r)
