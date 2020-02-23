# Generated by Django 3.0.3 on 2020-02-23 09:45

from django.conf import settings
from django.db import migrations, models
import django.db.models.deletion


class Migration(migrations.Migration):

    initial = True

    dependencies = [
        migrations.swappable_dependency(settings.AUTH_USER_MODEL),
    ]

    operations = [
        migrations.CreateModel(
            name='Workshop',
            fields=[
                ('name', models.CharField(max_length=256, primary_key=True, serialize=False)),
                ('vendor', models.CharField(max_length=256)),
                ('title', models.CharField(max_length=256)),
                ('description', models.TextField(max_length=1024)),
                ('url', models.CharField(max_length=512)),
            ],
        ),
        migrations.CreateModel(
            name='Session',
            fields=[
                ('name', models.CharField(max_length=256, primary_key=True, serialize=False)),
                ('id', models.CharField(max_length=64)),
                ('hostname', models.CharField(max_length=256)),
                ('secret', models.CharField(max_length=128)),
                ('reserved', models.BooleanField(default=False)),
                ('owner', models.ForeignKey(blank=True, null=True, on_delete=django.db.models.deletion.PROTECT, to=settings.AUTH_USER_MODEL)),
            ],
        ),
        migrations.CreateModel(
            name='Environment',
            fields=[
                ('name', models.CharField(max_length=256, primary_key=True, serialize=False)),
                ('sessions', models.ManyToManyField(to='workshops.Session')),
                ('workshop', models.ForeignKey(on_delete=django.db.models.deletion.PROTECT, to='workshops.Workshop')),
            ],
        ),
    ]
