# Generated migration for custom OAuth2 Application model

from django.conf import settings
from django.db import migrations


class Migration(migrations.Migration):

    dependencies = [
        ('workshops', '0015_environment_resource_name'),
        migrations.swappable_dependency(settings.OAUTH2_PROVIDER_APPLICATION_MODEL),
    ]

    operations = [
        # No database changes needed - the custom model inherits from
        # AbstractApplication and uses the same table structure. The swap
        # is handled via the swappable setting in settings.py.
    ]
