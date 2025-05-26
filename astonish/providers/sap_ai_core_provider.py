import os
import astonish.globals as globals
from gen_ai_hub.proxy.langchain.init_models import init_llm
from gen_ai_hub.proxy.core.proxy_clients import get_proxy_client
from astonish.providers.ai_provider_interface import AIProvider
from typing import List
from rich.prompt import Prompt, IntPrompt
from rich.panel import Panel
from rich.table import Table
from astonish.core.utils import console

class SAPAICoreProvider(AIProvider):
    def __init__(self):
        self.proxy_client = None

    def setup(self):
        console.print("[bold cyan]Setting up SAP AI Core...[/bold cyan]")
        
        defaults = {
            'client_id': ('', 'your-client-id'),
            'client_secret': ('', 'your-client-secret'),
            'auth_url': ('', 'https://<tenant-id>.authentication.sap.hana.ondemand.com'),
            'base_url': ('', 'https://api.ai.internalprod.eu-central-1.aws.ml.hana.ondemand.com/v2'),
            'resource_group': ('default', 'default')
        }

        # Load existing configuration if it exists
        if os.path.exists(globals.config_path):
            globals.config.read(globals.config_path)
        
        if 'SAP_AI_CORE' not in globals.config:
            globals.config['SAP_AI_CORE'] = {}

        # Input new values with rich library
        for key, (default, example) in defaults.items():
            current_value = globals.config['SAP_AI_CORE'].get(key, '')
            
            prompt_panel = Panel.fit(
                f"[bold magenta]{key.upper()}[/bold magenta]\n"
                f"[dim]Current:[/dim] [green]{current_value or 'None'}[/green]\n"
                f"[dim]Example:[/dim] [italic]{example}[/italic]",
                title="🔧 Configuration Input",
                border_style="cyan"
            )
            console.print(prompt_panel)

            new_value = Prompt.ask(
                f"[bold cyan]Enter value for {key}[/bold cyan] [dim](leave blank to keep current)[/dim]"
            ).strip()

            globals.config['SAP_AI_CORE'][key] = new_value if new_value else (current_value or default)

        os.makedirs(os.path.dirname(globals.config_path), exist_ok=True)
        os.makedirs(os.path.dirname(globals.config_path) + '/agents', exist_ok=True)
        with open(globals.config_path, 'w') as configfile:
            globals.config.write(configfile)

        globals.config.read(globals.config_path)
        self._initialize_proxy_client()

        # Get supported models
        supported_models = self.get_supported_models()

        # Display supported models in a table
        console.print("\n[bold yellow]Supported models:[/bold yellow]")
        for i, model in enumerate(supported_models, 1):
            console.print(f"{i}. {model}")

        while True:
            try:
                selection = IntPrompt.ask(
                    "\n[bold yellow]🔢 Select the number of the model you want to use as default[/bold yellow]"
                )
                if 1 <= selection <= len(supported_models):
                    default_model = supported_models[selection - 1]
                    break
                else:
                    console.print("[red]❌ Invalid selection. Please choose a number from the list.[/red]")
            except ValueError:
                console.print("[red]⚠️ Invalid input. Please enter a number.[/red]")

        # Ensure GENERAL section exists
        if 'GENERAL' not in globals.config:
            globals.config['GENERAL'] = {}

        globals.config['GENERAL']['default_provider'] = 'sap_ai_core'
        globals.config['GENERAL']['default_model'] = default_model

        # Write the configuration
        with open(globals.config_path, 'w') as configfile:
            globals.config.write(configfile)

        # Display success summary panel
        summary_table = Table(show_header=False, box=None)
        summary_table.add_row("🔌 Default Provider:", f"[bold green]{'sap_ai_core'}[/bold green]")
        summary_table.add_row("🤖 Default Model:", f"[bold blue]{default_model}[/bold blue]")

        console.print(Panel.fit(
            summary_table,
            title="✅ [bold green]Configuration Saved Successfully[/bold green]",
            border_style="green"
        ))

    def _initialize_proxy_client(self):
        if not os.path.exists(globals.config_path):
            raise FileNotFoundError("Configuration file not found. Please run setup() first.")
        
        globals.config.read(globals.config_path)

        # Set environment variables from the configuration
        os.environ["AICORE_AUTH_URL"] = globals.config.get('SAP_AI_CORE', 'auth_url')
        os.environ["AICORE_CLIENT_ID"] = globals.config.get('SAP_AI_CORE', 'client_id')
        os.environ["AICORE_CLIENT_SECRET"] = globals.config.get('SAP_AI_CORE', 'client_secret')
        os.environ["AICORE_RESOURCE_GROUP"] = globals.config.get('SAP_AI_CORE', 'resource_group')
        os.environ["AICORE_BASE_URL"] = globals.config.get('SAP_AI_CORE', 'base_url')

        # Initialize the proxy client
        self.proxy_client = get_proxy_client("gen-ai-hub")
        self.proxy_client.refresh_instance_cache()

    def get_supported_models(self) -> List[str]:
        if not self.proxy_client:
            self._initialize_proxy_client()

        model_names = [deployment.model_name for deployment in self.proxy_client.deployments]
        
        return sorted(list(set(model_names)))

    def get_llm(self, model_name: str, streaming: bool = True):
        if not self.proxy_client:
            self._initialize_proxy_client()

        # o1 and o3-mini require temperature=1
        if model_name in ["o1", "o3-mini"]:
            temperature = 1
        else:
            temperature = 0.7

        # Initialize and return the LLM
        llm = init_llm(
            model_name,
            proxy_client=self.proxy_client,
            streaming=streaming,
            max_tokens=8192,
            temperature=temperature
        )
        return llm
