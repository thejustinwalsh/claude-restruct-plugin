# Project Rules

## 3.2 Testing
All authentication changes must include unit tests.

## 4.1 Configuration
JWT configuration lives in config/auth.ts — do not hardcode values.

## 5.3 Token Refresh
Token refresh logic must use the existing RefreshTokenService.

## 7.0 Security
All auth changes require security review before merge.
