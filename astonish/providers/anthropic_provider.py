import os
import astonish.globals as globals
from langchain_anthropic import ChatAnthropic
from astonish.providers.ai_provider_interface import AIProvider
from typing import List
from rich.prompt import Prompt, IntPrompt
from rich.panel import Panel
from rich.table import Table
from astonish.core.utils import console

class AnthropicProvider(AIProvider):
    def __init__(self):
        self.api_key = None

    def setup(self):
        console.print("[bold cyan]Setting up Anthropic...[/bold cyan]")
        
        # Default values and examples
        defaults = {
            'api_key': ('', 'your-anthropic-api-key'),
        }

        # Load existing configuration if it exists
        if os.path.exists(globals.config_path):
            globals.config.read(globals.config_path)
        
        # Ensure ANTHROPIC section exists
        if 'ANTHROPIC' not in globals.config:
            globals.config['ANTHROPIC'] = {}

        # Input new values
        for key, (default, example) in defaults.items():
            current_value = globals.config['ANTHROPIC'].get(key, '')

            prompt_panel = Panel.fit(
                f"[bold magenta]{key.upper()}[/bold magenta]\n"
                f"[dim]Current:[/dim] [green]{current_value or 'None'}[/green]\n"
                f"[dim]Example:[/dim] [italic]{example}[/italic]",
                title="🔧 Configuration Input",
                border_style="cyan"
            )
            console.print(prompt_panel)

            # Inform user how to retain current value
            new_value = Prompt.ask(
                f"[bold cyan]Enter value for {key}[/bold cyan] [dim](leave blank to keep current)[/dim]"
            ).strip()

            globals.config['ANTHROPIC'][key] = new_value if new_value else (current_value or default)

        os.makedirs(os.path.dirname(globals.config_path), exist_ok=True)
        os.makedirs(os.path.dirname(globals.config_path) + '/agents', exist_ok=True)
        with open(globals.config_path, 'w') as configfile:
            globals.config.write(configfile)

        globals.config.read(globals.config_path)
        self._initialize_api_key()

        # Get supported models
        supported_models = self.get_supported_models()

        console.print("\n[bold yellow]Supported models:[/bold yellow]")
        for i, model in enumerate(supported_models, 1):
            console.print(f"{i}. {model}")

        # Ask user to select a default model
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
        
        # Add general section with default provider and model
        globals.config['GENERAL']['default_provider'] = 'anthropic'
        globals.config['GENERAL']['default_model'] = default_model

        # Write the configuration
        with open(globals.config_path, 'w') as configfile:
            globals.config.write(configfile)

        # Prepare summary table
        summary_table = Table(show_header=False, box=None)
        summary_table.add_row("🔌 Default Provider:", f"[bold green]{'anthropic'}[/bold green]")
        summary_table.add_row("🤖 Default Model:", f"[bold blue]{default_model}[/bold blue]")

        # Show success panel
        console.print(Panel.fit(
            summary_table,
            title="✅ [bold green]Configuration Saved Successfully[/bold green]",
            border_style="green"
        ))

    def _initialize_api_key(self):
        if not os.path.exists(globals.config_path):
            raise FileNotFoundError("Configuration file not found. Please run setup() first.")
        
        globals.config.read(globals.config_path)
        
        # Set API key from the configuration
        self.api_key = globals.config.get('ANTHROPIC', 'api_key')
        os.environ["ANTHROPIC_API_KEY"] = self.api_key

    def get_supported_models(self) -> List[str]:
        import requests
        
        if not self.api_key:
            self._initialize_api_key()

        url = "https://api.anthropic.com/v1/models"
        headers = {
            "x-api-key": self.api_key,
            "anthropic-version": "2023-06-01"
        }

        response = requests.get(url, headers=headers)
        
        if response.status_code != 200:
            raise Exception(f"Failed to fetch models: {response.status_code} - {response.text}")

        models = response.json()['data']
        model_names = [model['id'] for model in models]

        if not model_names:
            raise Exception("No models found from Anthropic API.")

        return model_names

    def get_llm(self, model_name: str, streaming: bool = True):
        if not self.api_key:
            self._initialize_api_key()

        # Initialize and return the LLM
        llm = ChatAnthropic(
            model=model_name,
            anthropic_api_key=self.api_key,
            streaming=streaming,
            max_tokens=8192,
            temperature=0.7
        )
        return llm
