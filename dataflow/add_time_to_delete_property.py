import datetime
import apache_beam as beam
from apache_beam.io.gcp.datastore.v1new import datastoreio, types
from apache_beam.options.pipeline_options import PipelineOptions, GoogleCloudOptions, StandardOptions

PROJECT_ID = 'webnote-184120'
DATASTORE_KIND = 'Notes'
STAGING_LOCATION = 'gs://webnote-dataflow/dataflow-staging'
TEMP_LOCATION = 'gs://webnote-dataflow/dataflow-temp'

# --- DoFn to Add/Update Property ---
class AddNewPropertyFn(beam.DoFn):
    """
    A DoFn that adds a 'time_to_delete' to each Datastore entity.
    If the property already exists, we skip the entity.
    """
    def process(self, entity):
        """
        Processes a single Datastore entity, adding or updating 'time_to_delete'.
        Args:
            entity (apache_beam.io.gcp.datastore.v1new.types.Entity): The Datastore entity to process.
        Yields:
            apache_beam.io.gcp.datastore.v1new.types.Entity: The modified entity.
        """
        # Dataflow's DatastoreIO reads/writes google.cloud.datastore.Entity objects.
        # These are mutable, so you can directly modify them.

        # Check if 'time_to_delete' exists or if its value is None.
        # You can customize this logic based on your exact requirements.
        if 'time_to_delete' not in entity.properties or entity.properties['time_to_delete'] is None:
            entity.properties['time_to_delete'] = entity.properties['time'] + datetime.timedelta(days=365*2) # Set your desired value here
            # You can also set values based on other properties of the entity:
            # entity['time_to_delete'] = f"Processed-{entity.key.id_or_name}"
            print(f"Adding time_to_delete to entity {entity.key}")
            yield entity
        else:
            print(f"Entity {entity.key} already has time_to_delete: {entity.properties['time_to_delete']}")
            # If you want to force an update even if it exists, uncomment the line below
            # entity['new_property'] = 'force_updated_value'
            # yield entity
            pass # No yield means the entity is not written back if unchanged


def run():
    """
    Defines and runs the Apache Beam pipeline to add a property to Datastore entities.
    """
    # --- Configure Pipeline Options ---
    options = PipelineOptions()
    # For running on Dataflow, set the runner to DataflowRunner
    options.view_as(StandardOptions).runner = 'DataflowRunner'
    # For local testing, you can use 'DirectRunner'
    # options.view_as(StandardOptions).runner = 'DirectRunner'

    # Configure Google Cloud specific options
    google_cloud_options = options.view_as(GoogleCloudOptions)
    google_cloud_options.project = PROJECT_ID
    google_cloud_options.staging_location = STAGING_LOCATION
    google_cloud_options.temp_location = TEMP_LOCATION
    # The Dataflow worker service account needs Datastore Editor and Storage Object Admin roles.
    # google_cloud_options.service_account_email = 'your-service-account@your-gcp-project-id.iam.gserviceaccount.com'

    # --- Create the Pipeline ---
    with beam.Pipeline(options=options) as pipeline:
        # 1. Read entities from Datastore
        # The query should specify the project and kind.
        # For simplicity, we'll read all entities of a given Kind.
        # For large datasets, consider using ancestor queries or cursors for better performance/split behavior.
        # You can also add filters to your query if you only want to update a subset of entities.
        # Note: apache_beam.io.gcp.datastore.v1new.datastoreio is used for modern Datastore I/O
        # It expects a google.cloud.datastore.query.Query object
        entities_to_process = (
            pipeline
            | 'ReadFromDatastore' >> datastoreio.ReadFromDatastore(
                types.Query(
                    kind=DATASTORE_KIND,
                    project=PROJECT_ID,
                )
            )
        )

        # 2. Transform: Add/Update the new property using a ParDo
        modified_entities = (
            entities_to_process
            | 'AddNewProperty' >> beam.ParDo(AddNewPropertyFn())
        )

        # 3. Write modified entities back to Datastore
        # DatastoreIO.WriteToDatastore performs an "upsert" operation.
        # If an entity with the same key exists, it will be updated; otherwise, it will be inserted.
        _ = (
            modified_entities
            | 'WriteToDatastore' >> datastoreio.WriteToDatastore(
                project=PROJECT_ID
            )
        )

if __name__ == '__main__':
    run()

# python add_time_to_delete_property.py