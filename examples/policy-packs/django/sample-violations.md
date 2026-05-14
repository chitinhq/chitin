# Django Sample Violations

These examples show the kinds of Django management commands the pack is intended to stop.

## 1. Flushing the database

Violation:

```bash
python manage.py flush --noinput
```

Expected fix:

```bash
python manage.py migrate
```

Reason: schema migration is allowed; deleting all rows is not.

## 2. Rolling an app back to zero

Violation:

```bash
python manage.py migrate billing zero
```

Expected fix:

```bash
python manage.py showmigrations billing
```

Reason: inspection is fine; destructive migration rollback needs an explicit human plan.

## 3. Full logical export

Violation:

```bash
python manage.py dumpdata > all-data.json
```

Expected fix:

```bash
python manage.py dumpdata billing.Invoice --indent 2 > invoices.json
```

Reason: targeted fixture work is reviewable; full-database export is usually too broad by default.
