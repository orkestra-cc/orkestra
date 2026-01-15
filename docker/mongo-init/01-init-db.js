// MongoDB initialization script for Orkestra
// This script runs when MongoDB container is first created

// Switch to the orkestra database
db = db.getSiblingDB('orkestra');

// Create application user with readWrite permissions
db.createUser({
  user: 'orkestra',
  pwd: 'orkestra_app_password_2024',
  roles: [
    {
      role: 'readWrite',
      db: 'orkestra'
    },
    {
      role: 'dbAdmin',
      db: 'orkestra'
    }
  ]
});

// Create collections with indexes
db.createCollection('users');
db.users.createIndex({ email: 1 }, { unique: true });
db.users.createIndex({ username: 1 }, { unique: true });
db.users.createIndex({ createdAt: -1 });

db.createCollection('operators');
db.operators.createIndex({ userId: 1 }, { unique: true });
db.operators.createIndex({ licenseNumber: 1 }, { unique: true });
db.operators.createIndex({ status: 1 });

db.createCollection('vehicles');
db.vehicles.createIndex({ registrationNumber: 1 }, { unique: true });
db.vehicles.createIndex({ operatorId: 1 });
db.vehicles.createIndex({ status: 1 });

db.createCollection('tracking_data');
db.tracking_data.createIndex({ vehicleId: 1, timestamp: -1 });
db.tracking_data.createIndex({ location: '2dsphere' });
db.tracking_data.createIndex({ timestamp: -1 });

db.createCollection('tasks');
db.tasks.createIndex({ assignedTo: 1, status: 1 });
db.tasks.createIndex({ createdAt: -1 });
db.tasks.createIndex({ dueDate: 1 });
db.tasks.createIndex({ priority: -1, status: 1 });

db.createCollection('reports');
db.reports.createIndex({ type: 1, createdAt: -1 });
db.reports.createIndex({ createdBy: 1 });
db.reports.createIndex({ dateRange: 1 });

db.createCollection('audit_logs');
db.audit_logs.createIndex({ userId: 1, timestamp: -1 });
db.audit_logs.createIndex({ action: 1, timestamp: -1 });
db.audit_logs.createIndex({ entityType: 1, entityId: 1 });

db.createCollection('sessions');
db.sessions.createIndex({ token: 1 }, { unique: true });
db.sessions.createIndex({ userId: 1 });
db.sessions.createIndex({ expiresAt: 1 }, { expireAfterSeconds: 0 });

// Insert default admin user
db.users.insertOne({
  _id: new ObjectId(),
  email: 'admin@orkestra.local',
  username: 'admin',
  password: '$2a$10$YourHashedPasswordHere', // This should be updated with proper bcrypt hash
  firstName: 'System',
  lastName: 'Administrator',
  role: 'admin',
  status: 'active',
  createdAt: new Date(),
  updatedAt: new Date()
});

print('Orkestra database initialized successfully!');