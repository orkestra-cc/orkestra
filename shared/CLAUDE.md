# Module: Shared - Data Models & Types
*Path: `/shared`*
*Parent: [../CLAUDE.md](../CLAUDE.md)*

<!-- Navigation -->
[← Root](../CLAUDE.md) | [☰ Module Map](../CLAUDE.md#module-map) | [🚀 Quick Start](../CLAUDE.md#quick-start)
<!-- /Navigation -->

## Module Purpose

The shared module contains **unified data models, types, and interfaces** used across all Orkestra services and applications to ensure consistency and type safety.

- **Primary Role**: Single source of truth for data structures and business rules
- **System Integration**: Provides type definitions for backend, frontend, and mobile applications
- **Architecture**: Language-agnostic models with platform-specific implementations

## Dependencies

### Imports
- **Standards**: JSON Schema, OpenAPI 3.1 specifications, industry validation patterns

### Importers
- **[`/backend/`](../backend/CLAUDE.md)** - Go structs and validation logic
- **[`/frontend/`](../frontend/CLAUDE.md)** - TypeScript interfaces and validation schemas
- **[`/mobile/`](../mobile/CLAUDE.md)** - Dart classes and serialization methods

## Overview

This module contains shared data models, types, and interfaces used across all Orkestra services and applications. All models must be kept in sync across backend (Go), frontend (TypeScript), and mobile (Dart).

## Core Data Models

### User Model
```json
{
  "_id": "ObjectId",
  "email": "string (unique, required)",
  "name": "string (required)",
  "avatar": "string (optional, URL)",
  "roles": ["developer", "super_admin", "administrator", "manager", "operator"],
  "permissions": ["users.read", "users.write", "vehicles.read", ...],
  "auth_provider": "google|apple|email",
  "metadata": {
    "phone": "string (optional)",
    "department": "string (optional)",
    "employee_id": "string (optional)"
  },
  "preferences": {
    "language": "en|es|fr",
    "timezone": "America/New_York",
    "notifications": {
      "email": true,
      "push": true,
      "sms": false
    }
  },
  "created_at": "timestamp",
  "updated_at": "timestamp",
  "last_login": "timestamp",
  "status": "active|suspended|deleted"
}
```

### Operator Model
```json
{
  "_id": "ObjectId",
  "user_id": "ObjectId (ref: users)",
  "license_number": "string (unique, required)",
  "license_expiry": "date",
  "vehicle_id": "ObjectId (ref: vehicles, optional)",
  "status": "available|on_duty|break|offline",
  "current_location": {
    "lat": "number",
    "lng": "number",
    "accuracy": "number (meters)",
    "timestamp": "timestamp",
    "speed": "number (km/h)",
    "heading": "number (degrees)"
  },
  "shift": {
    "start": "timestamp",
    "end": "timestamp",
    "break_start": "timestamp",
    "break_end": "timestamp"
  },
  "metrics": {
    "total_distance_today": "number (km)",
    "tasks_completed_today": "number",
    "average_speed": "number (km/h)",
    "idle_time": "number (minutes)",
    "driving_time": "number (minutes)"
  },
  "certifications": [
    {
      "type": "HAZMAT|CDL-A|CDL-B",
      "number": "string",
      "expiry": "date"
    }
  ],
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

### Vehicle Model
```json
{
  "_id": "ObjectId",
  "registration_number": "string (unique, required)",
  "vin": "string (unique, required)",
  "make": "string (required)",
  "model": "string (required)",
  "year": "number (required)",
  "type": "truck|van|car|motorcycle",
  "status": "active|maintenance|retired|reserved",
  "current_operator_id": "ObjectId (ref: operators, optional)",
  "last_location": {
    "lat": "number",
    "lng": "number",
    "timestamp": "timestamp",
    "speed": "number (km/h)",
    "heading": "number (degrees)",
    "engine_on": "boolean"
  },
  "specifications": {
    "fuel_type": "diesel|gasoline|electric|hybrid",
    "fuel_capacity": "number (liters)",
    "cargo_capacity": "number (kg)",
    "passenger_capacity": "number",
    "dimensions": {
      "length": "number (meters)",
      "width": "number (meters)",
      "height": "number (meters)"
    }
  },
  "telemetry": {
    "fuel_level": "number (percentage)",
    "odometer": "number (km)",
    "engine_hours": "number",
    "battery_voltage": "number",
    "tire_pressure": {
      "front_left": "number (psi)",
      "front_right": "number (psi)",
      "rear_left": "number (psi)",
      "rear_right": "number (psi)"
    },
    "engine_temperature": "number (celsius)"
  },
  "maintenance": {
    "last_service_date": "date",
    "next_service_date": "date",
    "service_interval_km": "number",
    "issues": [
      {
        "type": "engine|brake|tire|electrical|other",
        "description": "string",
        "severity": "low|medium|high|critical",
        "reported_date": "timestamp",
        "resolved_date": "timestamp"
      }
    ]
  },
  "insurance": {
    "provider": "string",
    "policy_number": "string",
    "expiry": "date",
    "coverage_type": "comprehensive|third_party"
  },
  "documents": [
    {
      "type": "registration|insurance|inspection",
      "file_url": "string",
      "expiry": "date"
    }
  ],
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

### Task Model
```json
{
  "_id": "ObjectId",
  "title": "string (required)",
  "description": "string (optional)",
  "type": "delivery|pickup|maintenance|inspection|other",
  "status": "pending|assigned|accepted|in_progress|completed|cancelled|failed",
  "priority": "low|medium|high|urgent",
  "assigned_to": "ObjectId (ref: operators, optional)",
  "vehicle_id": "ObjectId (ref: vehicles, optional)",
  "customer": {
    "name": "string",
    "contact": "string",
    "phone": "string",
    "email": "string"
  },
  "locations": {
    "pickup": {
      "address": "string",
      "lat": "number",
      "lng": "number",
      "notes": "string",
      "contact_person": "string",
      "contact_phone": "string"
    },
    "delivery": {
      "address": "string",
      "lat": "number",
      "lng": "number",
      "notes": "string",
      "contact_person": "string",
      "contact_phone": "string"
    },
    "waypoints": [
      {
        "address": "string",
        "lat": "number",
        "lng": "number",
        "arrival_time": "timestamp",
        "departure_time": "timestamp"
      }
    ]
  },
  "timeline": {
    "scheduled_at": "timestamp",
    "estimated_duration": "number (minutes)",
    "assigned_at": "timestamp",
    "accepted_at": "timestamp",
    "started_at": "timestamp",
    "completed_at": "timestamp",
    "cancelled_at": "timestamp"
  },
  "cargo": {
    "items": [
      {
        "description": "string",
        "quantity": "number",
        "weight": "number (kg)",
        "dimensions": "string",
        "special_handling": "fragile|hazmat|refrigerated|none"
      }
    ],
    "total_weight": "number (kg)",
    "total_volume": "number (m³)"
  },
  "notes": [
    {
      "text": "string",
      "author_id": "ObjectId (ref: users)",
      "timestamp": "timestamp",
      "type": "general|incident|customer"
    }
  ],
  "attachments": [
    {
      "type": "image|document|signature",
      "url": "string",
      "uploaded_by": "ObjectId (ref: users)",
      "uploaded_at": "timestamp",
      "description": "string"
    }
  ],
  "signature": {
    "image_url": "string",
    "signed_by": "string",
    "signed_at": "timestamp"
  },
  "rating": {
    "score": "number (1-5)",
    "comment": "string",
    "rated_by": "ObjectId (ref: users)",
    "rated_at": "timestamp"
  },
  "created_by": "ObjectId (ref: users)",
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

### Route Model
```json
{
  "_id": "ObjectId",
  "name": "string",
  "operator_id": "ObjectId (ref: operators)",
  "vehicle_id": "ObjectId (ref: vehicles)",
  "tasks": ["ObjectId (ref: tasks)"],
  "status": "planned|active|completed|cancelled",
  "date": "date",
  "optimization": {
    "distance": "number (km)",
    "duration": "number (minutes)",
    "fuel_cost": "number",
    "optimization_score": "number (0-100)"
  },
  "waypoints": [
    {
      "task_id": "ObjectId (ref: tasks)",
      "sequence": "number",
      "location": {
        "lat": "number",
        "lng": "number"
      },
      "estimated_arrival": "timestamp",
      "actual_arrival": "timestamp",
      "estimated_departure": "timestamp",
      "actual_departure": "timestamp",
      "status": "pending|arrived|completed|skipped"
    }
  ],
  "tracking": [
    {
      "lat": "number",
      "lng": "number",
      "timestamp": "timestamp",
      "speed": "number",
      "event": "start|waypoint|stop|break|end"
    }
  ],
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

## Enums and Constants

### User Roles
```typescript
enum UserRole {
  DEVELOPER = "developer",
  SUPER_ADMIN = "super_admin",
  ADMINISTRATOR = "administrator",
  MANAGER = "manager",
  OPERATOR = "operator"
}
```

### Permissions
```typescript
const PERMISSIONS = {
  // User permissions
  "users.read": "View users",
  "users.write": "Create/edit users",
  "users.delete": "Delete users",

  // Vehicle permissions
  "vehicles.read": "View vehicles",
  "vehicles.write": "Create/edit vehicles",
  "vehicles.delete": "Delete vehicles",

  // Task permissions
  "tasks.read": "View tasks",
  "tasks.write": "Create/edit tasks",
  "tasks.assign": "Assign tasks",
  "tasks.delete": "Delete tasks",

  // Report permissions
  "reports.read": "View reports",
  "reports.export": "Export reports",

  // System permissions
  "system.config": "System configuration",
  "system.backup": "System backup"
};
```

### Task Status
```typescript
enum TaskStatus {
  PENDING = "pending",
  ASSIGNED = "assigned",
  ACCEPTED = "accepted",
  IN_PROGRESS = "in_progress",
  COMPLETED = "completed",
  CANCELLED = "cancelled",
  FAILED = "failed"
}
```

### Vehicle Status
```typescript
enum VehicleStatus {
  ACTIVE = "active",
  MAINTENANCE = "maintenance",
  RETIRED = "retired",
  RESERVED = "reserved"
}
```

### Operator Status
```typescript
enum OperatorStatus {
  AVAILABLE = "available",
  ON_DUTY = "on_duty",
  BREAK = "break",
  OFFLINE = "offline"
}
```

## API Response Types

### Pagination Response
```typescript
interface PaginationResponse<T> {
  success: boolean;
  data: T[];
  metadata: {
    page: number;
    limit: number;
    total: number;
    totalPages: number;
    hasNext: boolean;
    hasPrev: boolean;
  };
  timestamp: string;
}
```

### Error Response
```typescript
interface ErrorResponse {
  success: false;
  error: {
    code: string;
    message: string;
    details?: Array<{
      field: string;
      message: string;
    }>;
  };
  request_id: string;
  timestamp: string;
}
```

### WebSocket Events
```typescript
interface WebSocketEvent {
  event: string;
  data: any;
  timestamp: string;
}

// Event Types
type VehicleUpdateEvent = {
  event: "vehicle_update";
  data: {
    vehicle_id: string;
    location: Location;
    telemetry?: VehicleTelemetry;
  };
};

type TaskUpdateEvent = {
  event: "task_update";
  data: {
    task_id: string;
    status: TaskStatus;
    operator_id?: string;
  };
};

type NotificationEvent = {
  event: "notification";
  data: {
    type: "info" | "warning" | "error";
    title: string;
    message: string;
    action?: string;
  };
};
```

## Validation Rules

### Email Validation
```regex
^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$
```

### Phone Validation
```regex
^\+?[1-9]\d{1,14}$  // E.164 format
```

### License Plate Validation
```regex
^[A-Z0-9]{2,10}$  // Alphanumeric, 2-10 characters
```

### VIN Validation
```regex
^[A-HJ-NPR-Z0-9]{17}$  // 17 characters, no I, O, Q
```

### Password Requirements
- Minimum 8 characters
- At least one uppercase letter
- At least one lowercase letter
- At least one number
- At least one special character

## Business Rules

### Task Assignment
1. Operator must be available (status: available)
2. Operator must have valid license
3. Vehicle must be active
4. Operator certifications must match task requirements
5. Vehicle capacity must meet cargo requirements

### Route Optimization
1. Minimize total distance
2. Respect time windows
3. Consider traffic patterns
4. Account for driver breaks
5. Optimize fuel consumption

### Shift Management
1. Maximum driving time: 11 hours
2. Mandatory break after 8 hours
3. Minimum rest period: 10 hours
4. Weekly driving limit: 60 hours

## Type Conversions

### Go Types
```go
type User struct {
    ID          primitive.ObjectID `bson:"_id,omitempty"`
    Email       string            `bson:"email"`
    Name        string            `bson:"name"`
    Avatar      *string           `bson:"avatar,omitempty"`
    Roles       []string          `bson:"roles"`
    Permissions []string          `bson:"permissions"`
    CreatedAt   time.Time         `bson:"created_at"`
    UpdatedAt   time.Time         `bson:"updated_at"`
    Status      string            `bson:"status"`
}
```

### TypeScript Types
```typescript
interface User {
  _id: string;
  email: string;
  name: string;
  avatar?: string;
  roles: UserRole[];
  permissions: string[];
  createdAt: Date;
  updatedAt: Date;
  status: "active" | "suspended" | "deleted";
}
```

### Dart Types
```dart
class User extends Equatable {
  final String id;
  final String email;
  final String name;
  final String? avatar;
  final List<String> roles;
  final List<String> permissions;
  final DateTime createdAt;
  final DateTime updatedAt;
  final String status;

  const User({
    required this.id,
    required this.email,
    required this.name,
    this.avatar,
    required this.roles,
    required this.permissions,
    required this.createdAt,
    required this.updatedAt,
    required this.status,
  });

  @override
  List<Object?> get props => [id, email, name, avatar, roles, permissions, createdAt, updatedAt, status];
}
```

## GraphQL Schema (Future)

```graphql
type User {
  id: ID!
  email: String!
  name: String!
  avatar: String
  roles: [UserRole!]!
  permissions: [String!]!
  createdAt: DateTime!
  updatedAt: DateTime!
  status: UserStatus!
}

enum UserRole {
  ADMIN
  MANAGER
  OPERATOR
  VIEWER
}

enum UserStatus {
  ACTIVE
  SUSPENDED
  DELETED
}

type Query {
  user(id: ID!): User
  users(page: Int, limit: Int): UserConnection!
}

type Mutation {
  createUser(input: CreateUserInput!): User!
  updateUser(id: ID!, input: UpdateUserInput!): User!
  deleteUser(id: ID!): Boolean!
}
```

## Data Migration

### Version Control
All model changes must be versioned and include migration scripts:
```
migrations/
├── v1.0.0_initial_schema.js
├── v1.1.0_add_user_preferences.js
├── v1.2.0_add_vehicle_telemetry.js
└── v2.0.0_restructure_tasks.js
```

### Migration Template
```javascript
// Migration: v1.1.0_add_user_preferences.js
module.exports = {
  version: "1.1.0",
  description: "Add user preferences field",

  up: async (db) => {
    await db.collection('users').updateMany(
      { preferences: { $exists: false } },
      {
        $set: {
          preferences: {
            language: "en",
            timezone: "UTC",
            notifications: { email: true, push: true, sms: false }
          }
        }
      }
    );
  },

  down: async (db) => {
    await db.collection('users').updateMany(
      {},
      { $unset: { preferences: "" } }
    );
  }
};
```

## Best Practices

1. **Consistency**: Keep models synchronized across all platforms
2. **Validation**: Validate at both client and server side
3. **Versioning**: Version all API changes
4. **Documentation**: Document all model changes
5. **Type Safety**: Use strong typing in all languages
6. **Null Safety**: Handle optional fields properly
7. **Timestamps**: Always include created_at and updated_at
8. **Soft Deletes**: Prefer status field over hard deletes
9. **Indexes**: Define indexes for frequently queried fields
10. **Normalization**: Balance between normalization and performance

## Module-Specific Guidelines

- **Cross-Platform Consistency**: Ensure identical data structures across Go, TypeScript, and Dart
- **Validation Rules**: Define comprehensive validation that works across all platforms
- **Type Safety**: Leverage strong typing in each target language
- **Schema Evolution**: Provide clear migration paths for model changes
- **Documentation**: Maintain detailed specifications for all data models
- **Testing**: Validate serialization/deserialization across all platforms
- **Performance**: Consider query patterns when designing data structures

---

### Related Guides
- [Project Overview](../CLAUDE.md) - System architecture and design principles
- [Backend Implementation](../backend/CLAUDE.md) - Go structs and MongoDB schemas
- [Frontend Types](../frontend/CLAUDE.md) - TypeScript interfaces and validation
- [Mobile Models](../mobile/CLAUDE.md) - Dart classes and serialization