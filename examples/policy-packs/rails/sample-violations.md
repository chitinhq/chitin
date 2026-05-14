# Rails Sample Violations

These examples focus on Rails commands that can cause disproportionate damage from an unattended agent session.

## 1. Destructive database reset

Violation:

```bash
bin/rails db:reset
```

Expected fix:

```bash
bin/rails db:migrate
```

Reason: schema evolution is allowed; dropping and recreating the database is not.

## 2. Editing encrypted credentials

Violation:

```bash
bin/rails credentials:edit
```

Expected fix:

```bash
bin/rails credentials:show
```

Reason: viewing or discussing credential needs is safer than mutating encrypted secrets in an agent session.

## 3. Broad gem update sweep

Violation:

```bash
bundle update
```

Expected fix:

```bash
bundle update devise
```

Reason: targeted gem bumps are reviewable; blanket updates tend to cause unrelated churn.
